---
name: tailscale-one-off-auth-key
description: Generate an actual one-off Tailscale auth key from the signed-in Admin Console in Arc, copy it to the macOS clipboard without exposing it, and verify the generated-key confirmation. Use for passwordless enrollment of one remote device into the current tailnet, especially temporary remote support; do not confuse auth keys with API access tokens, user invites, or device sharing.
---

# Tailscale One-Off Auth Key

Generate the `tskey-auth` credential itself through the existing signed-in Tailscale Admin Console. Do not require the user to pre-create an API access token, OAuth client, Keychain item, environment variable, or local helper credential.

Use `$arc-cdp-browser` and follow its required designated-worker contract. Work in the user's existing Arc session; do not launch a separate browser island.

## Required Outcome

Completion means all of the following are directly observed:

- The Tailscale dialog reports `Generated new key`.
- The key properties match the requested mode.
- The dialog's `Copy` control succeeds.
- A local clipboard check confirms auth-key shape without printing the secret.

Opening the Keys page, opening the generation form, preparing an API call, or asking the user to first create another credential is not completion.

## Authorization And Scope

Creating an auth key mutates the user's tailnet. The user's explicit request to generate one authorizes exactly one key creation. A request to explain, inspect, design, or package the capability does not authorize clicking the final `Generate key` button.

Open:

```text
https://console.tailscale.com/admin/settings/keys
```

Require the signed-in Admin Console. Verify the selected account or tailnet before generation. If multiple tailnets make the target ambiguous, stop and ask which one to use. If Arc redirects to the Tailscale login page, leave that page open and ask the user to sign in; do not choose another account or switch to an API-token implementation.

Use the `Auth keys` section and `Generate auth key…`. Do not use `API access tokens`, `Generate access token`, OAuth clients, user invites, or device sharing.

## Configure The Key

The established temporary remote-support baseline is:

- Description: a short audit label identifying the device or support session.
- Reusable: off. The resulting key must be `Single-use`.
- Expiration: 90 days.
- Ephemeral: on, so the temporary node is automatically removed after it goes offline.
- Tags: off, unless the user explicitly requests an existing tag-owned-device workflow.
- Pre-approved: enable only when the control is visible and the user wants immediate enrollment. If the control is absent, report that fact rather than implying it was enabled.

If the user explicitly requests a persistent node, keep `Reusable` off but turn `Ephemeral` off. One-off and ephemeral are independent properties; never enable `Reusable` for a request described as one-time or one-off.

Before the final click, re-read the form state and confirm it matches the requested properties. Then click `Generate key` exactly once. If the result is unclear, inspect the current dialog and Keys page; do not click again automatically.

## Copy And Verify Without Disclosure

In the `Generated new key` dialog:

1. Read back the displayed non-secret properties and expiry notice.
2. Click the dialog's `Copy` control.
3. Check the clipboard locally without printing its contents. It must begin with the Tailscale auth-key prefix `tskey-auth-` and be non-empty.

For example, the designated worker may run this verification; it emits only a boolean:

```bash
pbpaste | python3 -c 'import sys; value=sys.stdin.read(); print("valid_auth_key=" + str(value.startswith("tskey-auth-") and len(value) > len("tskey-auth-")))'
```

Never print, transcribe, screenshot, log, or place the key in chat, tracked files, shell arguments, or tool summaries. Report only that it is in the Mac clipboard, together with the verified mode and expiry. Leave the confirmation dialog open unless the user asks to close it.

## Enroll The Remote Device

Generating the key and enrolling the device are separate actions. Only proceed with enrollment when the user requests it or when it is already part of the active remote-support task.

For Windows temporary remote support, prompt for the clipboard value in an
Administrator PowerShell so the secret is not written into command history,
then enable unattended mode:

```powershell
$secureAuthKey = Read-Host "Tailscale auth key" -AsSecureString
$credential = [System.Net.NetworkCredential]::new("", $secureAuthKey)
& "$env:ProgramFiles\Tailscale\tailscale.exe" up --auth-key=$credential.Password --unattended=true
Remove-Variable credential, secureAuthKey
& "$env:ProgramFiles\Tailscale\tailscale.exe" ip -4
```

Do not put the actual key into Codex messages. After enrollment, verify the new device in the Machines page and confirm its Tailscale IP, online state, and expected `Ephemeral` badge when ephemeral mode was selected. A generated key alone does not prove device enrollment.

Authoritative behavior: [Tailscale auth keys](https://tailscale.com/docs/features/access-control/auth-keys), [secure auth-key handling](https://tailscale.com/docs/features/access-control/auth-keys/how-to/secure-auth-keys), and [Tailscale API](https://tailscale.com/api).
