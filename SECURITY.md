# Security policy

Report vulnerabilities privately to the repository owner before public
disclosure. Do not include Autodarts account credentials, camera frames, SSH
private keys, or `/var/lib/autodarts*` contents in reports.

The installer erases a disk only after displaying it and receiving the exact
`ERASE /dev/DEVICE` confirmation. Password SSH login is disabled. Users should
provide their own public key during installation.
