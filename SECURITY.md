# Security Policy

## Supported versions

Security fixes are provided for the latest published release. The default branch receives fixes intended for the next release but is not itself a supported release artifact.

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public GitHub issue, discussion, or pull request. Use GitHub's **Private vulnerability reporting** / **Security Advisories** interface for this repository. Include affected versions, reproduction steps, impact, and any known mitigations. If private reporting is unavailable, contact the repository owner privately through their verified GitHub profile and avoid including exploit details in public channels.

We will acknowledge a complete report, investigate it, and coordinate disclosure and remediation. No response-time SLA is promised for this initial release.

## Secrets and API keys

- Never commit real provider credentials, including in tests or examples.
- Prefer provider-specific environment variables for CI and ephemeral use.
- Lookup config files containing API keys are written with user-only permissions on Unix systems.
- Debug logs and issue reports must be reviewed for credentials before sharing.
- Revoke and rotate any credential that may have been exposed.
