# S3 WORM Store for Audit Log Anchoring

## Overview

This document describes the S3 Object Lock (WORM - Write Once Read Many) anchor system that provides external, tamper-evident proof of the audit log chain integrity.

## What Problem Does This Solve?

The audit log system uses a cryptographic chain (HMAC-SHA256) to make logs tamper-evident. However, this chain lives in PostgreSQL, which is:

- **Vulnerable to superuser access**: A rogue DBA or attacker with database access could theoretically modify the chain
- **Not externally verifiable**: Without an external anchor, there's no way to prove the chain hasn't been tampered with after the fact
- **Single point of trust**: The database is the only source of truth

**S3 Object Lock anchors solve this by:**

1. **External immutability**: Periodically writing chain checkpoints to S3 with COMPLIANCE mode Object Lock
2. **Non-repudiation**: Once written, anchors cannot be deleted or modified (even by AWS account owner)
3. **Independent verification**: External auditors can verify the chain without database access
4. **Regulatory compliance**: 7-year retention meets SOC 2, financial records, and audit requirements

## How It Works

### Anchor Cadence

**Hourly anchors** - provides tight windows for tamper detection while remaining cost-effective.

- **Time-based**: Every hour at :00 minutes (e.g., 00:00, 01:00, 02:00)
- **Event-based**: Also anchor every 10,000 events (whichever comes first)
- **On rotation**: Immediate anchor when rotating HMAC keys or starting a new chain

### Anchor Content

Each anchor is a lightweight JSON blob containing the chain head state:

```json
{
  "chain_id": "prod/global",
  "last_seq": 8723412,
  "last_hash": "b7e9f82a3c1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0",
  "ts_utc": "2025-10-17T14:00:00Z",
  "db_snapshot_lsn": "0/9F000A50",
  "prev_anchor_hash": "1d4f90e2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0"
}
```

**Fields:**

- `chain_id`: Environment identifier (e.g., "prod/global", "staging/us-west-2")
- `last_seq`: The `chain_index` of the last event in the chain
- `last_hash`: The `event_hash` of the last event (hex-encoded)
- `ts_utc`: UTC timestamp when this anchor was created
- `db_snapshot_lsn`: PostgreSQL LSN (Log Sequence Number) for point-in-time recovery verification
- `prev_anchor_hash`: SHA-256 of the previous anchor payload (creates an anchor-to-anchor chain)

### S3 Object Structure

Each anchor creates two objects:

```
s3://audit-anchors/prod/global/2025/10/17/14/anchor-20251017T140000Z.json
s3://audit-anchors/prod/global/2025/10/17/14/anchor-20251017T140000Z.json.sha256
```

**Path structure:**
```
s3://<bucket>/<env>/<region>/<year>/<month>/<day>/<hour>/anchor-<timestamp>.json
s3://<bucket>/<env>/<region>/<year>/<month>/<day>/<hour>/anchor-<timestamp>.json.sha256
```

**Why two files?**

- `.json` - The canonical anchor payload
- `.json.sha256` - SHA-256 hash of the JSON file (for quick verification without downloading full payload)

## Cost Analysis

For DocSpring's low-volume deployment with hourly anchors and 400-day retention:

| Item | Calculation | Annual Cost |
|------|-------------|-------------|
| Storage (steady state) | 24 anchors/day × 400 days × 2 files × 2 KB = 19.2 MB | ~$0.01 |
| PUT requests | 24 anchors/day × 365 days × 2 files = 17,520 PUTs @ $0.005/1k | ~$0.09 |
| KMS encryption (if enabled) | 17,520 encrypt requests @ $0.03/10k | ~$0.05 |
| GET requests (hourly verification) | 24 × 365 = 8,760 GETs @ $0.0004/1k | ~$0.00 |
| **Total** | | **~$0.15/year** |

**With replication to a second region:** ~$0.30/year

**Conclusion**: Essentially free for full tamper-evident audit chain anchoring. Over 400 days, total cost is under $0.20.

## AWS Setup

### 1. Create S3 Bucket with Object Lock

**CRITICAL**: Object Lock can ONLY be enabled at bucket creation time. It cannot be added later.

```bash
aws s3api create-bucket \
  --bucket audit-anchors \
  --region us-east-1 \
  --object-lock-enabled-for-bucket \
  --create-bucket-configuration LocationConstraint=us-east-1

aws s3api put-public-access-block \
  --bucket audit-anchors \
  --public-access-block-configuration \
    "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"

aws s3api put-bucket-versioning \
  --bucket audit-anchors \
  --versioning-configuration Status=Enabled
```

### 2. Configure Default Object Lock Retention

Set a 400-day default retention in COMPLIANCE mode (covers annual audits + buffer):

```bash
aws s3api put-object-lock-configuration \
  --bucket audit-anchors \
  --object-lock-configuration '{
    "ObjectLockEnabled": "Enabled",
    "Rule": {
      "DefaultRetention": {
        "Mode": "COMPLIANCE",
        "Days": 400
      }
    }
  }'
```

**COMPLIANCE mode**: Objects cannot be deleted or modified by anyone, including AWS account root. The only way to delete is to wait for retention to expire or contact AWS Support for bucket deletion.

**Alternative: GOVERNANCE mode** (NOT recommended for audit logs):
- Allows privileged users to override retention
- Weaker evidence for compliance/legal purposes

### 3. Enable Server-Side Encryption (SSE-KMS)

Create a dedicated KMS key for audit anchors:

```bash
aws kms create-key \
  --description "Audit anchor encryption key" \
  --key-policy '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Sid": "Enable IAM User Permissions",
        "Effect": "Allow",
        "Principal": {"AWS": "arn:aws:iam::ACCOUNT_ID:root"},
        "Action": "kms:*",
        "Resource": "*"
      },
      {
        "Sid": "Allow audit anchor writer",
        "Effect": "Allow",
        "Principal": {"AWS": "arn:aws:iam::ACCOUNT_ID:role/audit-anchor-writer"},
        "Action": ["kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey"],
        "Resource": "*"
      }
    ]
  }'

# Get the key ID from output
KMS_KEY_ID="arn:aws:kms:us-east-1:ACCOUNT_ID:key/KEY_ID"

aws s3api put-bucket-encryption \
  --bucket audit-anchors \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {
        "SSEAlgorithm": "aws:kms",
        "KMSMasterKeyID": "'"$KMS_KEY_ID"'"
      },
      "BucketKeyEnabled": true
    }]
  }'
```

### 4. Bucket Policy (Write-Only, Enforce Object Lock)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DenyUnencryptedObjectUploads",
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::audit-anchors/*",
      "Condition": {
        "StringNotEquals": {
          "s3:x-amz-server-side-encryption": "aws:kms"
        }
      }
    },
    {
      "Sid": "DenyObjectsWithoutObjectLock",
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::audit-anchors/*",
      "Condition": {
        "StringNotEquals": {
          "s3:x-amz-object-lock-mode": "COMPLIANCE"
        }
      }
    },
    {
      "Sid": "DenyDeleteOperations",
      "Effect": "Deny",
      "Principal": "*",
      "Action": [
        "s3:DeleteObject",
        "s3:DeleteObjectVersion"
      ],
      "Resource": "arn:aws:s3:::audit-anchors/*"
    }
  ]
}
```

Apply:

```bash
aws s3api put-bucket-policy --bucket audit-anchors --policy file://bucket-policy.json
```

### 5. IAM Role for Anchor Writer

Create a dedicated role with minimal permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:PutObjectRetention",
        "s3:PutObjectLegalHold"
      ],
      "Resource": "arn:aws:s3:::audit-anchors/*",
      "Condition": {
        "StringEquals": {
          "s3:x-amz-object-lock-mode": "COMPLIANCE",
          "s3:x-amz-server-side-encryption": "aws:kms"
        }
      }
    },
    {
      "Effect": "Allow",
      "Action": [
        "kms:Encrypt",
        "kms:Decrypt",
        "kms:GenerateDataKey"
      ],
      "Resource": "arn:aws:kms:us-east-1:ACCOUNT_ID:key/KEY_ID"
    }
  ]
}
```

### 6. IAM Role for Verification (Read-Only)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:GetObjectVersion",
        "s3:ListBucket",
        "s3:GetObjectRetention"
      ],
      "Resource": [
        "arn:aws:s3:::audit-anchors",
        "arn:aws:s3:::audit-anchors/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "kms:Decrypt"
      ],
      "Resource": "arn:aws:kms:us-east-1:ACCOUNT_ID:key/KEY_ID"
    }
  ]
}
```

### 7. Enable Cross-Region Replication (Optional but Recommended)

Create a second bucket in a different region and enable replication:

```bash
aws s3api create-bucket \
  --bucket audit-anchors-dr \
  --region us-west-2 \
  --object-lock-enabled-for-bucket \
  --create-bucket-configuration LocationConstraint=us-west-2

# Configure replication with Object Lock replication enabled
aws s3api put-bucket-replication \
  --bucket audit-anchors \
  --replication-configuration file://replication-config.json
```

**replication-config.json:**
```json
{
  "Role": "arn:aws:iam::ACCOUNT_ID:role/s3-replication-role",
  "Rules": [
    {
      "Status": "Enabled",
      "Priority": 1,
      "Filter": {},
      "Destination": {
        "Bucket": "arn:aws:s3:::audit-anchors-dr",
        "ReplicationTime": {
          "Status": "Enabled",
          "Time": {
            "Minutes": 15
          }
        },
        "Metrics": {
          "Status": "Enabled",
          "EventThreshold": {
            "Minutes": 15
          }
        }
      },
      "DeleteMarkerReplication": {
        "Status": "Disabled"
      }
    }
  ]
}
```

## Anchor Write Flow

### Pseudocode

```go
func WriteAnchor(db *Database, s3Client *s3.Client) error {
    // 1. Get latest event from chain
    latest, err := db.GetLatestEvent()
    if err != nil {
        return err
    }

    // 2. Get previous anchor hash (if exists)
    prevAnchor, _ := s3Client.GetLatestAnchor()
    prevAnchorHash := ""
    if prevAnchor != nil {
        prevAnchorHash = computeSHA256(prevAnchor.Payload)
    }

    // 3. Create anchor payload
    anchor := AnchorPayload{
        ChainID:        "prod/global",
        LastSeq:        latest.ChainIndex,
        LastHash:       hex.EncodeToString(latest.EventHash),
        TimestampUTC:   time.Now().UTC().Format(time.RFC3339),
        DBSnapshotLSN:  getCurrentLSN(db),
        PrevAnchorHash: prevAnchorHash,
    }

    // 4. Compute canonical JSON
    canonicalJSON := anchor.MarshalCanonical() // Deterministic JSON
    sha256Hash := computeSHA256(canonicalJSON)

    // 5. Generate S3 key with timestamp
    timestamp := time.Now().UTC()
    key := fmt.Sprintf("prod/global/%s/anchor-%s.json",
        timestamp.Format("2006/01/02/15"),
        timestamp.Format("20060102T150405Z"))

    // 6. Write JSON to S3 with Object Lock
    retainUntil := timestamp.Add(400 * 24 * time.Hour) // 400 days

    _, err = s3Client.PutObject(&s3.PutObjectInput{
        Bucket:                  aws.String("audit-anchors"),
        Key:                     aws.String(key),
        Body:                    bytes.NewReader(canonicalJSON),
        ServerSideEncryption:    aws.String("aws:kms"),
        SSEKMSKeyId:             aws.String("arn:aws:kms:..."),
        ObjectLockMode:          aws.String("COMPLIANCE"),
        ObjectLockRetainUntilDate: aws.Time(retainUntil),
        ChecksumSHA256:          aws.String(base64.StdEncoding.EncodeToString(sha256Hash)),
        IfNoneMatch:             aws.String("*"), // Prevent accidental overwrite
    })
    if err != nil {
        return fmt.Errorf("failed to write anchor: %w", err)
    }

    // 7. Write .sha256 file
    _, err = s3Client.PutObject(&s3.PutObjectInput{
        Bucket:                  aws.String("audit-anchors"),
        Key:                     aws.String(key + ".sha256"),
        Body:                    bytes.NewReader([]byte(hex.EncodeToString(sha256Hash))),
        ServerSideEncryption:    aws.String("aws:kms"),
        SSEKMSKeyId:             aws.String("arn:aws:kms:..."),
        ObjectLockMode:          aws.String("COMPLIANCE"),
        ObjectLockRetainUntilDate: aws.Time(retainUntil),
        IfNoneMatch:             aws.String("*"),
    })

    return err
}
```

### CLI Example

```bash
#!/bin/bash
set -euo pipefail

CHAIN_ID="prod/global"
BUCKET="audit-anchors"
KMS_KEY="arn:aws:kms:us-east-1:123456789012:key/KEY_ID"

# 1. Fetch latest event from DB
LATEST=$(psql -t -A -c "SELECT chain_index, encode(event_hash, 'hex') FROM audit.audit_event ORDER BY chain_index DESC LIMIT 1")
LAST_SEQ=$(echo "$LATEST" | cut -d'|' -f1)
LAST_HASH=$(echo "$LATEST" | cut -d'|' -f2)

# 2. Fetch previous anchor hash (if exists)
PREV_KEY=$(aws s3api list-objects-v2 --bucket "$BUCKET" --prefix "$CHAIN_ID/" --query 'Contents[-1].Key' --output text)
if [ "$PREV_KEY" != "None" ]; then
    PREV_ANCHOR=$(aws s3 cp "s3://$BUCKET/$PREV_KEY" -)
    PREV_HASH=$(echo "$PREV_ANCHOR" | sha256sum | awk '{print $1}')
else
    PREV_HASH=""
fi

# 3. Create anchor JSON
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
KEY="$CHAIN_ID/$(date -u +%Y/%m/%d/%H)/anchor-$(date -u +%Y%m%dT%H%M%SZ).json"
RETAIN_UNTIL=$(date -u -d '+400 days' +%Y-%m-%dT%H:%M:%SZ)

cat > /tmp/anchor.json <<EOF
{
  "chain_id": "$CHAIN_ID",
  "last_seq": $LAST_SEQ,
  "last_hash": "$LAST_HASH",
  "ts_utc": "$TIMESTAMP",
  "prev_anchor_hash": "$PREV_HASH"
}
EOF

# 4. Compute SHA256
SHA256=$(sha256sum /tmp/anchor.json | awk '{print $1}')
echo "$SHA256" > /tmp/anchor.json.sha256

# 5. Upload JSON with Object Lock
aws s3api put-object \
  --bucket "$BUCKET" \
  --key "$KEY" \
  --body /tmp/anchor.json \
  --server-side-encryption aws:kms \
  --ssekms-key-id "$KMS_KEY" \
  --object-lock-mode COMPLIANCE \
  --object-lock-retain-until-date "$RETAIN_UNTIL" \
  --checksum-sha256 "$(openssl dgst -sha256 -binary /tmp/anchor.json | base64)"

# 6. Upload .sha256 file
aws s3api put-object \
  --bucket "$BUCKET" \
  --key "$KEY.sha256" \
  --body /tmp/anchor.json.sha256 \
  --server-side-encryption aws:kms \
  --ssekms-key-id "$KMS_KEY" \
  --object-lock-mode COMPLIANCE \
  --object-lock-retain-until-date "$RETAIN_UNTIL"

echo "Anchor written: s3://$BUCKET/$KEY"
echo "Chain head: seq=$LAST_SEQ hash=$LAST_HASH"
```

## Verification Flow

### Hourly Verification Job

```go
func VerifyChain(db *Database, s3Client *s3.Client) error {
    // 1. Fetch latest anchor from S3
    anchor, err := s3Client.GetLatestAnchor("prod/global")
    if err != nil {
        return fmt.Errorf("failed to fetch anchor: %w", err)
    }

    // 2. Verify anchor JSON matches .sha256
    anchorHash := computeSHA256(anchor.Payload)
    storedHash, _ := s3Client.GetObject(anchor.Key + ".sha256")
    if !bytes.Equal(anchorHash, storedHash) {
        return fmt.Errorf("ANCHOR TAMPERED: hash mismatch")
    }

    // 3. Fetch chain head from database
    latest, err := db.GetEventAtIndex(anchor.LastSeq)
    if err != nil {
        return fmt.Errorf("failed to fetch event %d: %w", anchor.LastSeq, err)
    }

    // 4. Compare hashes
    expectedHash := hex.EncodeToString(latest.EventHash)
    if anchor.LastHash != expectedHash {
        return fmt.Errorf("CHAIN TAMPERED: DB shows %s, anchor shows %s",
            expectedHash, anchor.LastHash)
    }

    // 5. Verify chain integrity from last anchor to current head
    currentHead, _ := db.GetLatestEvent()
    if err := db.VerifyChainIntegrity(anchor.LastSeq, currentHead.ChainIndex); err != nil {
        return fmt.Errorf("CHAIN BROKEN: %w", err)
    }

    // 6. Verify anchor-to-anchor chain
    if anchor.PrevAnchorHash != "" {
        prevAnchor, _ := s3Client.GetPreviousAnchor(anchor)
        expectedPrevHash := computeSHA256(prevAnchor.Payload)
        if anchor.PrevAnchorHash != hex.EncodeToString(expectedPrevHash) {
            return fmt.Errorf("ANCHOR CHAIN BROKEN: prev anchor hash mismatch")
        }
    }

    return nil // Chain is valid
}
```

### Manual Verification

```bash
#!/bin/bash
# Verify the audit chain against S3 anchors

BUCKET="audit-anchors"
CHAIN_ID="prod/global"

# 1. Get latest anchor
LATEST_KEY=$(aws s3api list-objects-v2 \
  --bucket "$BUCKET" \
  --prefix "$CHAIN_ID/" \
  --query 'reverse(sort_by(Contents, &LastModified))[-1].Key' \
  --output text)

echo "Latest anchor: s3://$BUCKET/$LATEST_KEY"

# 2. Download anchor
aws s3 cp "s3://$BUCKET/$LATEST_KEY" /tmp/anchor.json
aws s3 cp "s3://$BUCKET/$LATEST_KEY.sha256" /tmp/anchor.json.sha256

# 3. Verify anchor integrity
COMPUTED_HASH=$(sha256sum /tmp/anchor.json | awk '{print $1}')
STORED_HASH=$(cat /tmp/anchor.json.sha256)

if [ "$COMPUTED_HASH" != "$STORED_HASH" ]; then
    echo "❌ ANCHOR TAMPERED: hash mismatch"
    exit 1
fi

echo "✅ Anchor integrity verified"

# 4. Extract chain head from anchor
LAST_SEQ=$(jq -r '.last_seq' /tmp/anchor.json)
LAST_HASH=$(jq -r '.last_hash' /tmp/anchor.json)

echo "Anchor chain head: seq=$LAST_SEQ hash=$LAST_HASH"

# 5. Verify against database
DB_EVENT=$(psql -t -A -c "SELECT encode(event_hash, 'hex') FROM audit.audit_event WHERE chain_index = $LAST_SEQ")

if [ "$DB_EVENT" != "$LAST_HASH" ]; then
    echo "❌ CHAIN TAMPERED: DB hash=$DB_EVENT, anchor hash=$LAST_HASH"
    exit 1
fi

echo "✅ Chain verified against anchor"

# 6. Verify database chain integrity from anchor to current head
CURRENT_SEQ=$(psql -t -A -c "SELECT MAX(chain_index) FROM audit.audit_event")
echo "Verifying chain from $LAST_SEQ to $CURRENT_SEQ..."

BROKEN=$(psql -t -A -c "SELECT broken_at_index FROM audit.verify_chain($LAST_SEQ, $CURRENT_SEQ) LIMIT 1")

if [ -n "$BROKEN" ] && [ "$BROKEN" != "" ]; then
    echo "❌ CHAIN BROKEN at index $BROKEN"
    exit 1
fi

echo "✅ Full chain integrity verified"
```

## Monitoring and Alerting

### CloudWatch Alarms

1. **Missing Anchor Alarm**
   - Trigger: No anchor written in last 2 hours (2× expected cadence)
   - Action: Page on-call engineer

2. **Verification Failure Alarm**
   - Trigger: Verification job returns non-zero exit code
   - Action: Page security team immediately

3. **Replication Lag Alarm** (if using CRR)
   - Trigger: Replication lag > 30 minutes
   - Action: Alert DevOps team

### Verification Schedule

- **Hourly**: Automated verification job (runs at :15 of each hour)
- **Daily**: Full chain verification from genesis to current head
- **Weekly**: Verify anchor-to-anchor chain integrity
- **On-demand**: After any security incident or database maintenance

## Lifecycle Management

### Transition to Lower-Cost Storage

After 30 days, transition anchors to Glacier Deep Archive:

```bash
aws s3api put-bucket-lifecycle-configuration \
  --bucket audit-anchors \
  --lifecycle-configuration '{
    "Rules": [
      {
        "Id": "archive-old-anchors",
        "Status": "Enabled",
        "Filter": {},
        "Transitions": [
          {
            "Days": 30,
            "StorageClass": "GLACIER_IR"
          },
          {
            "Days": 90,
            "StorageClass": "DEEP_ARCHIVE"
          }
        ],
        "NoncurrentVersionTransitions": [
          {
            "NoncurrentDays": 30,
            "StorageClass": "DEEP_ARCHIVE"
          }
        ]
      }
    ]
  }'
```

**Cost impact**: Storage cost drops from $0.023/GB to $0.00099/GB (~96% reduction).

### Retention Expiration

After 400 days, Object Lock retention expires and objects can be deleted if needed. For regulatory compliance:

- **SOC 2 Type II**: 400 days exceeds the typical 1-year requirement
- **Extended retention**: For specific regulations (SOX, FINRA), extend to 3-7 years as needed
- **Cost**: Even with 7-year retention, total cost is under $1/year

**Recommendation**: Start with 400 days. Extend to longer retention only if required by specific compliance frameworks.

## Security Considerations

### Break-Glass Procedures

In the event of a security incident requiring deletion of audit logs:

1. **Cannot delete locked objects**: Object Lock COMPLIANCE mode prevents deletion
2. **Only option**: Delete entire bucket (requires AWS Support ticket)
3. **Recommended**: Do NOT delete. Use Legal Hold to mark objects for investigation instead.

### Legal Hold

To mark objects for legal/investigation purposes without affecting retention:

```bash
aws s3api put-object-legal-hold \
  --bucket audit-anchors \
  --key "prod/global/2025/10/17/14/anchor-20251017T140000Z.json" \
  --legal-hold Status=ON
```

Legal Hold is independent of retention and requires explicit removal.

### Audit S3 Access

Enable S3 access logging and CloudTrail to track all access to the anchor bucket:

```bash
aws s3api put-bucket-logging \
  --bucket audit-anchors \
  --bucket-logging-status '{
    "LoggingEnabled": {
      "TargetBucket": "s3-access-logs",
      "TargetPrefix": "audit-anchors/"
    }
  }'
```

## Compliance and Evidence

### SOC 2 Type II

S3 Object Lock anchors provide strong evidence for:

- **CC6.1** (Logical Access Security): Immutable audit trail
- **CC7.2** (System Monitoring): Continuous verification
- **CC7.3** (Security Incident Management): Tamper-evident logs

### Financial Records Retention

Default 400-day retention meets most requirements:

- **SOC 2 Type II**: Typically 1 year (exceeded by 400 days)
- **GDPR**: Right to erasure exceptions for legal obligations
- **SOX/FINRA**: If needed, extend retention to 3-7 years (still < $1/year cost)

**Note**: For financial services or public companies, configure longer retention as required by your specific regulations.

### External Auditor Access

Provide read-only IAM credentials to auditors:

```bash
# Create temporary read-only credentials (expires after 12 hours)
aws sts get-session-token --duration-seconds 43200

# Auditor can verify anchors without database access
aws s3 ls s3://audit-anchors/prod/global/ --profile auditor
aws s3 cp s3://audit-anchors/prod/global/2025/10/17/14/anchor-20251017T140000Z.json - --profile auditor
```

## Future Enhancements

### Public Notarization

For additional non-repudiation, publish anchor hashes to a public transparency log:

- **GitHub Releases**: Automated commit of daily anchor hashes
- **Blockchain**: Post to Ethereum or similar immutable ledger
- **AWS QLDB**: Append to Amazon Quantum Ledger Database

### Asymmetric Signing

Sign anchors with KMS asymmetric keys for stronger non-repudiation:

```bash
# Create ECC signing key
aws kms create-key --key-spec ECC_NIST_P256 --key-usage SIGN_VERIFY

# Sign anchor payload
aws kms sign \
  --key-id "arn:aws:kms:..." \
  --message-type RAW \
  --signing-algorithm ECDSA_SHA_256 \
  --message fileb:///tmp/anchor.json \
  --output text --query Signature | base64 -d > /tmp/anchor.json.sig

# Upload .sig alongside .json and .sha256
```

## Implementation Checklist

- [ ] Create S3 bucket with Object Lock enabled
- [ ] Configure 7-year default retention in COMPLIANCE mode
- [ ] Enable SSE-KMS with dedicated CMK
- [ ] Apply bucket policy (deny delete, enforce lock, enforce encryption)
- [ ] Create IAM role for anchor writer (write-only)
- [ ] Create IAM role for verification (read-only)
- [ ] Set up cross-region replication (optional)
- [ ] Implement anchor write job (hourly cron or scheduled Lambda)
- [ ] Implement verification job (hourly cron, alerts on failure)
- [ ] Configure CloudWatch alarms (missing anchors, verification failures)
- [ ] Enable S3 access logging and CloudTrail
- [ ] Document anchor bucket location in runbooks
- [ ] Test anchor write and verification flows
- [ ] Test failure scenarios (tampered DB, missing anchors)
- [ ] Train team on break-glass procedures

## References

- [S3 Object Lock Overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- [Using S3 Object Lock with COMPLIANCE Mode](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock-compliance-mode.html)
- [S3 Object Lock Pricing](https://aws.amazon.com/s3/pricing/) (no additional charge)
- [PostgreSQL Continuous Archiving](https://www.postgresql.org/docs/current/continuous-archiving.html)
