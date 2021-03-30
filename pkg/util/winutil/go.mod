module github.com/DataDog/datadog-agent/pkg/util/winutil

go 1.14

replace github.com/DataDog/datadog-agent/pkg/util/log => ../log

require (
	github.com/DataDog/datadog-agent/pkg/util/log v0.0.0
	github.com/client9/misspell v0.3.4
	github.com/frapposelli/wwhrd v0.2.4
	github.com/fzipp/gocyclo v0.3.1
	github.com/golangci/golangci-lint v1.27.0
	github.com/gordonklaus/ineffassign v0.0.0-20210103220932-664217a59c00
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b
	golang.org/x/sys v0.0.0-20200930185726-fdedc70b468f
	gotest.tools/gotestsum v0.5.3
	honnef.co/go/tools v0.0.1-2020.1.5
)
