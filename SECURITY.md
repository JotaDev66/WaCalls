# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories: open the
repository's **Security** tab and choose **Report a vulnerability**. Please do not
open public issues or pull requests for security problems.

Reports are acknowledged on a best-effort basis within 7 days.

## Supported versions

The latest release and the `develop` branch receive security fixes.

## Deployment hardening

WaCalls ships safe-by-default for trusted LANs, but production deployments should
follow the Security section of the [README](./README.md#security):

- Set `WACALLS_API_TOKEN` to require a bearer token on every `/api` and `/debug` route.
- Restrict `WACALLS_CORS_ORIGINS` to the browser origins that need access.
- Treat `wacalls.db` as secret material: it holds WhatsApp session credentials.
