# Security policy

Ballast sends load to other people's servers and keeps their credentials, so
security reports are taken seriously.

## Reporting a vulnerability

**Please do not open a public issue, pull request or discussion for a
vulnerability.**

Use GitHub's private reporting instead: on the repository page, open the
**Security** tab and choose **Report a vulnerability**. Your report is visible
only to the maintainers.

Include, as far as you can:

- what you found, and where (file, endpoint, or the screen);
- how to reproduce it, with the smallest example you have;
- what an attacker could do with it.

You will get an answer within a few days. We will keep you informed while it is
fixed, and credit you in the release notes if you wish.

## What is in scope

- Anything that lets one organization read or change another's data.
- Anything that sends load to a target whose ownership has not been verified, or
  to a different target than the one verified (including through redirects, DNS
  tricks or the load generator's network).
- Leaking a secret, a token or a credential: through the API, the logs, the
  stored results or an import.
- Bypassing the limits on virtual users, duration, rate or concurrency.
- Authentication and session weaknesses.

## What is out of scope

- Findings that need a physical machine, or a compromised account of the person
  reporting them.
- Denial of service by volume against a public deployment: report what lets a
  small request cause a large effect, not the fact that traffic can be sent.
- Reports from automated scanners with no demonstrated impact.

## Supported versions

Ballast is under active development: only the latest version on `main` receives
fixes.
