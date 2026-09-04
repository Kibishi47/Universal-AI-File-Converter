#!/bin/sh
set -e

echo "Waiting for SeaweedFS Master & Filer to be ready..."
sleep 3

# Check SeaweedFS S3 endpoint
echo "Configuring SeaweedFS S3 bucket 'conversions'..."

# Create bucket via curl if needed
curl -s -X PUT http://seaweedfs:8333/conversions || true

echo "SeaweedFS S3 bucket initialized successfully."
