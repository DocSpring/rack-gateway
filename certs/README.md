# Certificates

`rds-global-bundle.pem` is Amazon RDS's certificate bundle for every region, from
`https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem` (published 2026-10-14, SHA-256
`90a6f1a7978a1b1b096dca6c57511ca48d3b8cf052f8e704baf358f34ea2f21b`). The image ships it as
`/app/certs/rds-global-bundle.pem`, and `convox.yml` points `PGSSLROOTCERT` at it, so the gateway verifies an RDS
database's certificate and hostname (`PGSSLMODE=verify-full`).

It's kept in the repository rather than downloaded at build time: AWS replaces the file in place, which would change
what a rebuilt release trusts. To update it, download it again (only a version published at least 7 days ago) and
update the date and checksum above.
