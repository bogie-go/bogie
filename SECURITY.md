# Security

## Reporting a vulnerability

Please don't open a public issue for a security problem.

Report it privately through GitHub: on the repository's **Security** tab,
choose **Report a vulnerability**
(<https://github.com/bogie-go/bogie/security/advisories/new>). Only the
project's maintainers see the report, and the fix can be discussed in that advisory
before anything is public.

If that option isn't available, open an issue that says you have something to
report privately, with no details, and a maintainer will arrange a private
channel.

Replies may take a few days.

## What counts

- Bogie itself: the `bogie` CLI and the commands it runs.
- Code Bogie generates: a template that produces an insecure app (for example
  the credentials handling, the authentication middleware from
  `bogie g authentication`, the Dockerfile or the Kamal config).

A vulnerability in a dependency (Gin, pgx, sqlc, goose, River, Kamal) belongs
with that project. If Bogie pins or generates an affected version, a report
here is still welcome.

## Supported versions

Fixes go into the next release. Only the latest release gets fixes; today
that is v0.1.0.
