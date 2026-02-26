package main

import (
	"fmt"
	"github.com/absmartly/go-sdk/sdk"
	"io/ioutil"
	"os"
)

func main() {
	var clientConfig = sdk.ClientConfig{
		Endpoint:    "https://acme.absmartly.io/v1",
		APIKey:      os.Getenv("ABSMARTLY_APIKEY"),
		Application: os.Getenv(`ABSMARTLY_APPLICATION`),
		Environment: os.Getenv(`ABSMARTLY_ENVIRONMENT`),
	}

	var sdkConfig = sdk.ABSmartlyConfig{Client: sdk.CreateDefaultClient(clientConfig)}

	var sd = sdk.Create(sdkConfig)

	var contextConfig = sdk.ContextConfig{
		Units: map[string]string{
			"session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
			"user_id":    "123456",
		}, PublishDelay: 10000, RefreshInterval: 5000}

	var ctx = sd.CreateContext(contextConfig)
	ctx.WaitUntilReady()

	var path, _ = os.Getwd()
	var content, _ = ioutil.ReadFile(path + "/sdk/testAssets/context.json")
	var deser = sdk.DefaultContextDataDeserializer{}
	var data, _ = deser.Deserialize(content)
	var anotherContextConfig = sdk.ContextConfig{
		Units: map[string]string{
			"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
			"user_id":    "123456789",
			"email":      "bleh@absmartly.com",
		}}

	var anotherCtx = sd.CreateContextWith(anotherContextConfig, data)
	fmt.Println(anotherCtx.IsReady())
	fmt.Println(anotherCtx.GetTreatment("exp_test_fullon"))

	var treatment, _ = ctx.GetTreatment("exp_test_ab")
	fmt.Println(treatment)
	fmt.Println(ctx.GetData())
	var properties = map[string]interface{}{
		"value": 125,
		"fee":   125,
	}

	var err = ctx.Track("payment", properties)
	fmt.Println(err)

	ctx.Close()

}
