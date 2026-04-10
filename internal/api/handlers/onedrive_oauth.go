package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

// Environment variable names for embedded OneDrive OAuth credentials.
const (
	envOneDriveClientID     = "VAULT_ONEDRIVE_CLIENT_ID"
	envOneDriveClientSecret = "VAULT_ONEDRIVE_CLIENT_SECRET" //nolint:gosec // env var name, not a credential

	// onedriveBackupFolderName is the folder name automatically created
	// in the user's OneDrive to store Vault backups.
	onedriveBackupFolderName = "Vault Backups"
)

// Microsoft identity platform endpoints for personal (consumer) accounts.
var msOneDriveEndpoint = oauth2.Endpoint{ //nolint:gosec // OAuth endpoint URLs, not credentials
	AuthURL:  "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize",
	TokenURL: "https://login.microsoftonline.com/consumers/oauth2/v2.0/token",
}

type onedriveAuthURLRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

type onedriveExchangeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// resolveOneDriveCredentials returns the embedded client_id and client_secret
// from environment variables.
func resolveOneDriveCredentials() (string, string) {
	return os.Getenv(envOneDriveClientID), os.Getenv(envOneDriveClientSecret)
}

// OneDriveStatus reports whether embedded OneDrive OAuth credentials are
// configured.
//
//	GET /api/v1/replication/onedrive/status
func (h *ReplicationHandler) OneDriveStatus(w http.ResponseWriter, _ *http.Request) {
	id, secret := resolveOneDriveCredentials()
	respondJSON(w, http.StatusOK, map[string]bool{
		"configured": id != "" && secret != "",
	})
}

// OneDriveAuthURL generates a Microsoft OAuth2 consent URL.
// Uses Vault's built-in OAuth credentials (from environment variables).
//
//	POST /api/v1/replication/onedrive/auth-url
func (h *ReplicationHandler) OneDriveAuthURL(w http.ResponseWriter, r *http.Request) {
	var req onedriveAuthURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.RedirectURI == "" {
		respondError(w, http.StatusBadRequest, "redirect_uri is required")
		return
	}

	clientID, clientSecret := resolveOneDriveCredentials()
	if clientID == "" || clientSecret == "" {
		respondError(w, http.StatusBadRequest,
			"OneDrive is not configured. Set VAULT_ONEDRIVE_CLIENT_ID and VAULT_ONEDRIVE_CLIENT_SECRET environment variables.")
		return
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  req.RedirectURI,
		Scopes:       []string{"Files.ReadWrite", "offline_access", "User.Read"},
		Endpoint:     msOneDriveEndpoint,
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

// OneDriveExchangeToken exchanges an OAuth2 authorisation code for a refresh
// token, retrieves the user's email, and finds or creates a "Vault Backups"
// folder in OneDrive. Returns all connection details needed by the frontend.
//
//	POST /api/v1/replication/onedrive/exchange-token
func (h *ReplicationHandler) OneDriveExchangeToken(w http.ResponseWriter, r *http.Request) {
	var req onedriveExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Code == "" || req.RedirectURI == "" {
		respondError(w, http.StatusBadRequest, "code and redirect_uri are required")
		return
	}

	clientID, clientSecret := resolveOneDriveCredentials()
	if clientID == "" || clientSecret == "" {
		respondError(w, http.StatusBadRequest, "OneDrive credentials not available")
		return
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  req.RedirectURI,
		Scopes:       []string{"Files.ReadWrite", "offline_access", "User.Read"},
		Endpoint:     msOneDriveEndpoint,
	}

	ctx := context.Background()
	token, err := cfg.Exchange(ctx, req.Code)
	if err != nil {
		log.Printf("onedrive token exchange failed: %v", err)
		respondError(w, http.StatusBadRequest, "token exchange failed")
		return
	}

	if token.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "no refresh token received; try re-authorising with prompt=consent")
		return
	}

	httpClient := cfg.Client(ctx, token)

	// Fetch user profile email.
	email := onedriveGetUserEmail(httpClient)

	// Find or create the "Vault Backups" folder.
	folderID, driveID, err := onedriveEnsureBackupFolder(httpClient)
	if err != nil {
		log.Printf("onedrive: failed to ensure backup folder: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create backup folder in OneDrive")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"refresh_token": token.RefreshToken,
		"email":         email,
		"folder_id":     folderID,
		"drive_id":      driveID,
		"folder_name":   onedriveBackupFolderName,
	})
}

const graphBaseURLHandlers = "https://graph.microsoft.com/v1.0"

// onedriveGetUserEmail fetches the user's email from Microsoft Graph /me endpoint.
func onedriveGetUserEmail(client *http.Client) string {
	resp, err := client.Get(graphBaseURLHandlers + "/me")
	if err != nil {
		log.Printf("onedrive: failed to fetch user profile: %v", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var profile struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.Unmarshal(body, &profile); err != nil {
		return ""
	}
	if profile.Mail != "" {
		return profile.Mail
	}
	return profile.UserPrincipalName
}

// onedriveEnsureBackupFolder finds or creates a "Vault Backups" folder
// in the root of the user's OneDrive. Returns (folderItemID, driveID, error).
func onedriveEnsureBackupFolder(client *http.Client) (string, string, error) {
	// Try to get existing folder by path.
	checkURL := fmt.Sprintf("%s/me/drive/root:/%s", graphBaseURLHandlers, onedriveBackupFolderName)
	resp, err := client.Get(checkURL)
	if err != nil {
		return "", "", fmt.Errorf("check for backup folder: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var item struct {
			ID        string `json:"id"`
			ParentRef struct {
				DriveID string `json:"driveId"`
			} `json:"parentReference"`
		}
		if err := json.Unmarshal(body, &item); err != nil {
			return "", "", fmt.Errorf("parse folder response: %w", err)
		}
		return item.ID, item.ParentRef.DriveID, nil
	}

	// Folder doesn't exist — create it.
	createURL := graphBaseURLHandlers + "/me/drive/root/children"
	createBody, _ := json.Marshal(map[string]any{
		"name":                              onedriveBackupFolderName,
		"folder":                            map[string]any{},
		"@microsoft.graph.conflictBehavior": "fail",
	})

	resp2, err := client.Post(createURL, "application/json", byteReader(createBody))
	if err != nil {
		return "", "", fmt.Errorf("create backup folder: %w", err)
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return "", "", fmt.Errorf("read create response: %w", err)
	}

	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("create folder failed (HTTP %d): %s", resp2.StatusCode, string(body2))
	}

	var created struct {
		ID        string `json:"id"`
		ParentRef struct {
			DriveID string `json:"driveId"`
		} `json:"parentReference"`
	}
	if err := json.Unmarshal(body2, &created); err != nil {
		return "", "", fmt.Errorf("parse created folder: %w", err)
	}
	return created.ID, created.ParentRef.DriveID, nil
}

// byteReader wraps a byte slice in an io.Reader for http.Client.Post.
func byteReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}

// OneDriveCallback handles the OAuth2 redirect from Microsoft after the user
// grants consent. It renders a small HTML page that posts the authorisation
// code back to the opener window via postMessage, then closes itself.
//
//	GET /api/v1/replication/onedrive/callback
func (h *ReplicationHandler) OneDriveCallback(w http.ResponseWriter, r *http.Request) {
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

	data := onedriveCallbackData{Code: code, Error: errParam, ExpectedOrigin: expectedOrigin}
	if errParam != "" {
		data.Success = false
	} else if code == "" {
		data.Success = false
		data.Error = "no authorisation code received"
	} else {
		data.Success = true
	}

	if err := onedriveCallbackTmpl.Execute(w, data); err != nil {
		log.Printf("onedrive callback template error: %v", err)
	}
}

type onedriveCallbackData struct {
	Success        bool
	Code           string
	Error          string
	ExpectedOrigin string
}

var onedriveCallbackTmpl = template.Must(template.New("onedrive-callback").Parse(`<!DOCTYPE html>
<html><head><title>Vault - OneDrive</title></head>
<body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0">
<div style="text-align:center">
{{if .Success}}
  <h2 style="color:#22c55e">&#10003; Authorization Successful</h2>
  <p>Connecting to OneDrive...</p>
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
    window.opener.postMessage({type:'onedrive-auth-code',code:{{.Code}}},{{.ExpectedOrigin}});
    setTimeout(function(){window.close()},2000);
  }
{{else}}
  if(window.opener){
    window.opener.postMessage({type:'onedrive-auth-error',error:{{.Error}}},{{.ExpectedOrigin}});
  }
  setTimeout(function(){window.close()},3000);
{{end}}
})();
</script>
</body></html>`))
