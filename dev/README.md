# Development helpers

Files here support local development and testing. Nothing in this directory is
used by a running server.

## `seaweedfs-s3.json`

Credentials for the S3-compatible server the object-store tests run against.

The two S3 storage backends are tested against a real server rather than a
mock, because a mock would only prove that the code calls the functions the
test expects it to call. What needs proving is that both clients behave the
same way against a store an operator would really run — that a missing object
surfaces the same error from each, that a presigned URL can be followed by a
browser with no credentials, that a declared size is honoured.

Start one:

```
docker run -d --name tdc-s3 -p 8333:8333 \
  -v "$PWD/dev/seaweedfs-s3.json:/etc/seaweedfs/s3.json:ro" \
  chrislusf/seaweedfs:latest \
  server -s3 -s3.config=/etc/seaweedfs/s3.json -dir=/data
```

Then point the tests at it:

```
TDC_TEST_S3_ENDPOINT=http://127.0.0.1:8333 go test ./internal/storage/
```

The tests skip when that variable is unset, so this is optional — a contributor
without Docker is not blocked, and the local-filesystem backend, which is the
default and what most deployments use, is tested without any of this.

The credentials here are fixed and public on purpose: they reach a throwaway
container holding test fixtures. Never reuse them for anything else.
