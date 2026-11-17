package sdk

// SDKName and SDKVersion identify this SDK to the collector. SDKAgent is sent
// as the X-Agent header ("absmartly-go-sdk/<version>"), so the collector can
// tell which SDK release produced a request. SDKVersion must match the release
// tag without its "v" prefix; the release workflow checks it.
const (
	SDKName    = "absmartly-go-sdk"
	SDKVersion = "1.2.0"
	SDKAgent   = SDKName + "/" + SDKVersion
)
