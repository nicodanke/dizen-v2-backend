# Backup image (PRD-18 RF-9).
#
# Built on the same Postgres image the databases run, and that is not a detail: pg_dump
# refuses to dump a server newer than itself, so pinning them together means the day the
# databases are upgraded the backup cannot silently keep dumping with an old client.
#
# rclone rather than the AWS CLI: one static binary instead of a Python runtime, and it
# speaks S3, R2 and MinIO with the same configuration, which is what lets the restore drill
# run against a local MinIO.
FROM postgres:18.6-alpine

RUN apk add --no-cache rclone ca-certificates tzdata

# rclone is configured entirely from the environment, so there is no config file and it
# should stop announcing that on every single call: a log full of expected notices is a log
# nobody reads.
ENV RCLONE_CONFIG=/dev/null

COPY backup.sh restore.sh prune.sh entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/backup.sh \
             /usr/local/bin/restore.sh \
             /usr/local/bin/prune.sh \
             /usr/local/bin/entrypoint.sh

# Dumps are written here before being uploaded, and deleted right after. It is a scratch
# area, not a second copy: a backup that only exists on the same server it protects is not
# a backup (RF-9).
WORKDIR /tmp/backup

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["schedule"]
