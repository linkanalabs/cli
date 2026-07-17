package manifest

import _ "embed"

// embedded is the vendored copy of the backend-generated CLI manifest. It is
// refreshed via `make update-manifest` and shipped inside the binary.
//
//go:embed cli-manifest.json
var embedded []byte
