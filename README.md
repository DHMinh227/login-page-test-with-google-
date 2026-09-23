# Garden Guardians Google Login Template

A small, working Google OAuth 2.0 + OpenID Connect login server for an original lane-defense game. It runs on `localhost`, accepts real Google accounts, validates Google's signed ID token on the Go server, and creates a separate HttpOnly game session.

It requests only these OpenID scopes:

- `openid`
- `email`
- `profile`

It does **not** request Gmail API access and cannot read email messages.

## How the login works

1. The browser opens `GET /auth/google/start`.
2. The Go server creates `state`, `nonce`, and a PKCE verifier.
3. The browser is redirected to Google's real sign-in page.
4. Google redirects to `http://localhost:8080/auth/google/callback`.
5. The Go server exchanges the authorization code and validates the ID token signature, issuer, audience, expiry, nonce, and verified-email claim.
6. The server creates its own opaque `gg_session` cookie. The raw Google token is not used as the game session.
7. `GET /api/me` returns the signed-in player profile.

## One-time Google Cloud setup

You must create the Client ID and Client Secret in your own Google account. Do not send the secret to anyone and do not commit `.env`.

1. Open [Google Cloud Console](https://console.cloud.google.com/) and create or select a project.
2. Open **Google Auth Platform**.
3. Under **Branding**, set an app name such as `Garden Guardians`, choose a support email, and save.
4. Under **Audience**, choose **External**. Keep the app in **Testing** while developing.
5. Add your real Gmail address under **Test users**. Only listed test users can sign in while the app is in Testing.
6. Under **Clients**, choose **Create client**.
7. Choose application type **Web application**.
8. Add this exact **Authorized redirect URI**:

   ```text
   http://localhost:8080/auth/google/callback
   ```

9. Create the client and copy its **Client ID** and **Client Secret**.

The scheme, host, port, path, and trailing slash must match exactly. `http://localhost` is allowed for local development; a deployed app should use HTTPS.

## Configure and run

From PowerShell in this folder:

```powershell
Copy-Item .env.example .env
notepad .env
```

Paste your values:

```dotenv
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your-client-secret
APP_URL=http://localhost:8080
LISTEN_ADDR=:8080
COOKIE_SECURE=false
```

Then run:

```powershell
& 'C:\Program Files\Go\bin\go.exe' mod download
& 'C:\Program Files\Go\bin\go.exe' run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080), select **Continue with Google**, and choose the Gmail account you added as a test user.

## Quick checks

Before Google is configured, this still shows the login design and setup message:

```powershell
& 'C:\Program Files\Go\bin\go.exe' run ./cmd/server
```

Health check:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
```

Automated tests:

```powershell
$env:GOCACHE = "$PWD\.codex-cache\go-build"
& 'C:\Program Files\Go\bin\go.exe' test -count=1 ./...
```

## Local prototype boundary

This first version deliberately stores sessions in memory. Restarting the Go server signs everybody out. That is good for a simple local prototype, but production should move users and sessions to PostgreSQL or another durable store, run behind HTTPS, set `COOKIE_SECURE=true`, rotate secrets, add account deletion/privacy pages, and apply rate limits.

## Later Unity integration

Do not put `GOOGLE_CLIENT_SECRET` in a Unity build; players can extract it. Keep Google login on this backend.

The recommended next phase is:

1. Unity opens the system browser to the backend's `/game-login/start` endpoint.
2. The backend completes Google OpenID Connect in the browser.
3. The backend creates a short-lived, single-use game ticket.
4. The browser returns that ticket to Unity through a registered custom URL such as `gardenguardians://login?...` or a loopback callback.
5. Unity exchanges the one-time ticket for your own game access and refresh tokens.

That ticket flow is intentionally not in this browser-only MVP. Add it only when the Unity project is ready, because custom URL registration differs by Windows, macOS, Android, and iOS.

## Common errors

- `redirect_uri_mismatch`: the callback in Google Cloud is not exactly `http://localhost:8080/auth/google/callback`.
- `access_denied`: the user cancelled, or their email is not in the test-user list.
- App starts but login is disabled: `.env` is missing or the two Google credential values are blank.
- Cookie works locally but not after deployment: use a real HTTPS domain, update `APP_URL` and the Google redirect URI, and set `COOKIE_SECURE=true`.
