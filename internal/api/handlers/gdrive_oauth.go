package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	goauth2 "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

// Environment variable names for embedded Google Drive OAuth credentials.
// When set, users can connect to Google Drive without providing their own
// Cloud Console credentials.
const (
	envGDriveClientID     = "VAULT_GDRIVE_CLIENT_ID"
	envGDriveClientSecret = "VAULT_GDRIVE_CLIENT_SECRET" //nolint:gosec // env var name, not a credential

	// gdriveBackupFolderName is the folder name automatically created
	// in the user's Google Drive to store Vault backups.
	gdriveBackupFolderName = "Vault Backups"
)

type gdriveAuthURLRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type gdriveExchangeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// resolveGDriveCredentials returns the embedded client_id and client_secret
// from environment variables.
func resolveGDriveCredentials() (string, string) {
	return os.Getenv(envGDriveClientID), os.Getenv(envGDriveClientSecret)
}

// GDriveStatus reports whether embedded Google Drive OAuth credentials are
// configured, allowing users to connect without their own Cloud Console credentials.
//
//	GET /api/v1/replication/gdrive/status
func (h *ReplicationHandler) GDriveStatus(w http.ResponseWriter, _ *http.Request) {
	id, secret := resolveGDriveCredentials()
	respondJSON(w, http.StatusOK, map[string]bool{
		"configured": id != "" && secret != "",
	})
}

// GDriveAuthURL generates a Google OAuth2 consent URL.
// Uses Vault's built-in OAuth credentials (from environment variables).
//
//	POST /api/v1/replication/gdrive/auth-url
func (h *ReplicationHandler) GDriveAuthURL(w http.ResponseWriter, r *http.Request) {
	var req gdriveAuthURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.RedirectURI == "" {
		respondError(w, http.StatusBadRequest, "redirect_uri is required")
		return
	}

	clientID, clientSecret := resolveGDriveCredentials()
	if clientID == "" || clientSecret == "" {
		respondError(w, http.StatusBadRequest,
			"Google Drive is not configured. Set VAULT_GDRIVE_CLIENT_ID and VAULT_GDRIVE_CLIENT_SECRET environment variables.")
		return
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  req.RedirectURI,
		Scopes:       []string{drive.DriveFileScope, "openid", "email"},
		Endpoint:     google.Endpoint,
	}

	state, err := generateOAuthState()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	url := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	respondJSON(w, http.StatusOK, map[string]string{"url": url, "state": state})
}

// GDriveExchangeToken exchanges an OAuth2 authorisation code for a refresh
// token, retrieves the user's email, and finds or creates a "Vault Backups"
// folder in Google Drive. Returns all connection details needed by the frontend.
//
//	POST /api/v1/replication/gdrive/exchange-token
func (h *ReplicationHandler) GDriveExchangeToken(w http.ResponseWriter, r *http.Request) {
	var req gdriveExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Code == "" || req.RedirectURI == "" {
		respondError(w, http.StatusBadRequest, "code and redirect_uri are required")
		return
	}

	clientID, clientSecret := resolveGDriveCredentials()
	if clientID == "" || clientSecret == "" {
		respondError(w, http.StatusBadRequest, "Google Drive credentials not available")
		return
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  req.RedirectURI,
		Scopes:       []string{drive.DriveFileScope, "openid", "email"},
		Endpoint:     google.Endpoint,
	}

	ctx := context.Background()
	token, err := cfg.Exchange(ctx, req.Code)
	if err != nil {
		log.Printf("gdrive token exchange failed: %v", err)
		respondError(w, http.StatusBadRequest, "token exchange failed")
		return
	}

	if token.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "no refresh token received; try re-authorising with prompt=consent")
		return
	}

	// Fetch the user's email address.
	httpClient := cfg.Client(ctx, token)
	email := ""
	oauth2Svc, err := goauth2.NewService(ctx, option.WithHTTPClient(httpClient))
	if err == nil {
		userinfo, uiErr := oauth2Svc.Userinfo.Get().Do()
		if uiErr == nil {
			email = userinfo.Email
		} else {
			log.Printf("gdrive: failed to fetch user info: %v", uiErr)
		}
	} else {
		log.Printf("gdrive: failed to create oauth2 service: %v", err)
	}

	// Find or create the "Vault Backups" folder.
	driveSvc, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		log.Printf("gdrive: failed to create drive service: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to connect to Google Drive")
		return
	}

	folderID, err := gdriveEnsureBackupFolder(driveSvc)
	if err != nil {
		log.Printf("gdrive: failed to ensure backup folder: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create backup folder in Google Drive")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"refresh_token": token.RefreshToken,
		"email":         email,
		"folder_id":     folderID,
		"folder_name":   gdriveBackupFolderName,
	})
}

// gdriveEnsureBackupFolder finds or creates a folder named "Vault Backups"
// in the root of the user's Google Drive. Only app-created folders are matched.
func gdriveEnsureBackupFolder(svc *drive.Service) (string, error) {
	// Search for existing folder created by this app.
	q := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and 'root' in parents and trashed = false",
		gdriveBackupFolderName)
	result, err := svc.Files.List().Q(q).Fields("files(id, name)").PageSize(1).Do()
	if err != nil {
		return "", fmt.Errorf("search for backup folder: %w", err)
	}
	if len(result.Files) > 0 {
		return result.Files[0].Id, nil
	}

	// Create the folder.
	folder := &drive.File{
		Name:     gdriveBackupFolderName,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{"root"},
	}
	created, err := svc.Files.Create(folder).Fields("id").Do()
	if err != nil {
		return "", fmt.Errorf("create backup folder: %w", err)
	}
	return created.Id, nil
}

// GDriveCallback handles the OAuth2 redirect from Google after the user grants
// consent. It renders a small HTML page that posts the authorisation code back
// to the opener window via postMessage, then closes itself.
//
//	GET /api/v1/replication/gdrive/callback
func (h *ReplicationHandler) GDriveCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Derive the expected origin from the request
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	expectedOrigin := scheme + "://" + host

	data := gdriveCallbackData{Code: code, Error: errParam, ExpectedOrigin: expectedOrigin}
	if errParam != "" {
		data.Success = false
	} else if code == "" {
		data.Success = false
		data.Error = "no authorisation code received"
	} else {
		data.Success = true
	}

	if err := gdriveCallbackTmpl.Execute(w, data); err != nil {
		log.Printf("gdrive callback template error: %v", err)
	}
}

type gdriveCallbackData struct {
	Success        bool
	Code           string
	Error          string
	ExpectedOrigin string
}

var gdriveCallbackTmpl = template.Must(template.New("gdrive-callback").Parse(`<!DOCTYPE html>
<html><head><title>Vault - Google Drive</title></head>
<body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0">
<div style="text-align:center">
{{if .Success}}
  <h2 style="color:#22c55e">&#10003; Authorization Successful</h2>
  <p>Connecting to Google Drive...</p>
  <p style="color:#888">This window will close automatically.</p>
{{else}}
  <h2 style="color:#ef4444">Authorization Failed</h2>
  <p>{{.Error}}</p>
  <p style="color:#888">You can close this window.</p>
{{end}}
</div>
<script>
(function(){
{{if .Success}}
  if(window.opener){
    window.opener.postMessage({type:'gdrive-auth-code',code:{{.Code}}},{{.ExpectedOrigin}});
    setTimeout(function(){window.close()},2000);
  }
{{else}}
  if(window.opener){
    window.opener.postMessage({type:'gdrive-auth-error',error:{{.Error}}},{{.ExpectedOrigin}});
  }
  setTimeout(function(){window.close()},3000);
{{end}}
})();
</script>
</body></html>`))

// generateOAuthState returns a cryptographically random state string for CSRF protection.
func generateOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
