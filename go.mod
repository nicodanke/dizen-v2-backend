// Root module of the repository. It holds no production code: it exists to fix the import
// path prefix shared by every module in the workspace.
module github.com/nicodanke/dizen-v2-backend

go 1.27.0

require gopkg.in/yaml.v3 v3.0.1

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
