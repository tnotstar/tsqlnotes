# Security Profile - tsqlnotes

This document defines the security parameters, threat profile, and compliance controls for the `tsqlnotes` web application.

## STRIDE Threat Matrix

| Threat Category | Threat Description | Mitigation Strategy |
| :--- | :--- | :--- |
| **Spoofing** | Session hijacking or credentials spoofing | Force HTTPS only. Generate secure cookies with `HttpOnly`, `Secure`, and `SameSite` flags. |
| **Tampering** | Parameter tampering or SQL Injection | Enforce parameterized queries using Go standard driver bindings. Implement strict Content Security Policies (CSP). |
| **Repudiation** | Missing log records for access controls | Log note mutations and login attempts via structured logs containing client IP references. |
| **Information Disclosure** | Leak of database tables via error output | Trap sql errors and return generic error tokens to the client. Redact sensitive user data in logs. |
| **Denial of Service** | HTTP flood or large payload OOM | Restrict request body sizes using `http.MaxBytesReader` (max 2MB per upload). Set strict write/read connection timeouts. |
| **Elevation of Privilege** | Horizontal or vertical privilege escalation | Validate note ownership prior to serving any edit or deletion request. |

## Compliance Controls (C1 - C11)

### C1: Secure Transport
- Reject unencrypted HTTP traffic. Enforce TLS 1.3 for all listener ports.

### C2: Rate Limiting
- Enforce IP-based rate limiting on sensitive routes (like `/login` or `/api/`) utilizing a bounded cache.

### C3: Authentication & Token Security
- Validate user sessions via cryptographically signed JWT tokens or secure HTTP-only session cookies. Invalidate tokens on logout.

### C4: Authorization
- Deny-by-default access control model. Route access is mapped strictly to roles. Validate resource ownership on every transaction.

### C5: Identifiers
- Relational tables must map resource identifiers using non-sequential UUIDv7, preventing enumeration attacks on note URIs.

### C6: Request Hygiene
- Bind connection limits on the SQL database pools. Set HTTP server write/read timeouts to a maximum of 15 seconds.

### C7: Containment & Runtime
- Container runtimes must utilize scratch/minimal base images. Execute processes under a non-root UID.

### C8: Secrets Management
- All configuration credentials (including DB passwords and token signing keys) must be loaded dynamically from the host environment, never checked in to source control.

### C9: Structured Logging
- Use standard Go `log/slog` structured logging with JSON handler. Do not capture raw HTTP request payloads containing passwords or note contents.

### C10: Supply Chain & CI/CD
- Verified and checked dependencies in `go.sum`. Vulnerability checks via Trivy.

### C11: Graceful Teardown
- Listen to termination signals (`SIGTERM`, `SIGINT`). Close active network connections gracefully via `http.Server.Shutdown` (timeout 10s) and close database connections cleanly.
