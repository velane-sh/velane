# Security Policy

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability.

Send reports to [abhi@velane.sh](mailto:abhi@velane.sh). Include:

- the affected component and version or commit;
- steps to reproduce the issue;
- the impact you believe it has;
- any suggested mitigation, if you have one.

Avoid including secrets, customer data, or credentials in the report. We will
acknowledge a report within seven days and keep you updated as we investigate.

Please allow time for a fix and coordinated release before discussing the issue
publicly. We will credit reporters in the release notes unless they prefer to
remain anonymous.

## Supported versions

Security fixes are made on the default branch and included in the next release.
When a vulnerability affects a published release, the advisory will identify
the affected versions and the first version containing the fix.

Older releases may not receive backported fixes. Users should update to the
latest available release.

## Scope

Reports concerning the Velane source code, published container images, hosted
services, authentication, tenant isolation, executor sandboxing, and dependency
or supply-chain vulnerabilities are welcome.

Reports about vulnerabilities in third-party services should be sent to the
maintainer of that service unless the issue is caused by how Velane integrates
with it.
