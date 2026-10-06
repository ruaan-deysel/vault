import { test, expect } from '@playwright/test';
import { setupVaultMockApi } from './mocks/vault-api';

test.describe('Settings Configuration & Management', () => {
  test.beforeEach(async ({ page }) => {
    await setupVaultMockApi(page);
  });

  test('switches across tabs and navigates settings sections', async ({ page }) => {
    await page.goto('/#/settings');

    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();

    // Verify main tabs are present
    const generalTab = page.getByRole('button', { name: 'General', exact: true });
    const securityTab = page.getByRole('button', { name: 'Security', exact: true });
    const notificationsTab = page.getByRole('button', { name: 'Notifications', exact: true });
    const referenceTab = page.getByRole('button', { name: 'Reference', exact: true });

    await expect(generalTab).toBeVisible();
    await expect(securityTab).toBeVisible();
    await expect(notificationsTab).toBeVisible();
    await expect(referenceTab).toBeVisible();

    // Switch to Security tab
    await securityTab.click();
    await expect(page.getByText('Encryption').first()).toBeVisible();
    await expect(page.getByText('API Key').first()).toBeVisible();

    // Switch to Reference tab
    await referenceTab.click();
    await expect(page.getByText('Documentation').first()).toBeVisible();

    // Switch back to General tab
    await generalTab.click();
    await expect(page.getByText('Appearance').first()).toBeVisible();
  });

  test('toggles dark and light appearance modes', async ({ page }) => {
    await page.goto('/#/settings');

    // Appearance mode options
    const lightBtn = page.getByRole('button', { name: /light/i }).first();
    const darkBtn = page.getByRole('button', { name: /dark/i }).first();

    if (await lightBtn.isVisible() && await darkBtn.isVisible()) {
      await lightBtn.click();
      await expect(page.locator('html')).toHaveClass(/light/);

      await darkBtn.click();
      await expect(page.locator('html')).not.toHaveClass(/light/);
    }
  });

  test('configures and tests Discord notification webhook', async ({ page }) => {
    await page.goto('/#/settings');

    // Go to Notifications tab
    await page.getByRole('button', { name: 'Notifications' }).click();
    await expect(page.getByText('Discord Notifications')).toBeVisible();

    // Enter Webhook URL
    const discordInput = page.locator('#discord-url');
    await expect(discordInput).toBeVisible();
    await discordInput.fill('https://discord.com/api/webhooks/123456/abcdef');

    // Click test button next to webhook input
    const testBtn = discordInput.locator('xpath=following-sibling::button');
    await expect(testBtn).toBeEnabled();
    await testBtn.click();

    // Verify toast confirms test notification dispatched
    await expect(page.getByText(/Test notification sent to Discord!/i)).toBeVisible();
  });

  test('triggers diagnostics bundle export download', async ({ page }) => {
    await page.goto('/#/settings');

    const diagDownloadBtn = page.getByRole('button', { name: /Download Diagnostics/i });
    if (await diagDownloadBtn.isVisible()) {
      const downloadPromise = page.waitForEvent('download');
      await diagDownloadBtn.click();
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toContain('diagnostics');
    }
  });
});
