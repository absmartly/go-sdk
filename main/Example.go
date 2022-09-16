package main

import (
	"fmt"
	"os"
)

func main() {
	var clientConfig = ClientConfig{
		Endpoint_:    "https://acme.absmartly.io/v1",
		ApiKey_:      os.Getenv("ABSMARTLY_APIKEY"),
		Application_: os.Getenv("ABSMARTLY_APIKEY"),
		Environment_: os.Getenv("ABSMARTLY_APIKEY"),
	}

	var sdkConfig = ABSmartlyConfig{Client_: CreateDefaultClient(clientConfig)}

	var sdk = Create(sdkConfig)

	var contextConfig = ContextConfig{Units_: map[string]string{
		"session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
		"user_id":    "123456",
	}}

	var buff [512]byte
	var block [16]int32
	var st [4]int32
	var assignBuf [12]int8
	var ctx = sdk.CreateContext(contextConfig, buff, block, st).WaitUntilReady()

	//time.Sleep(2 * time.Second)
	var treatment, _ = ctx.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	fmt.Println(treatment)

	var properties = map[string]interface{}{
		"value": 125,
		"fee":   125,
	}

	_ = ctx.Track("payment", properties, buff, block, st)

	ctx.Close(buff, block, st)

}
