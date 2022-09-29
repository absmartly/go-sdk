package main

import (
	"fmt"
	"github.com/absmartly/go-sdk/sdk"
	"io/ioutil"
	"os"
)

func main() {
	var clientConfig = sdk.ClientConfig{
		Endpoint_:    "https://sandbox.absmartly.io/v1",
		ApiKey_:      "R54OsOwR9wwtWYhDXwdtp4iuD3pvtIUoHz4BakCuBl0t3E9kA0R6jzTW6cffQC9O",
		Application_: "www",  // created in the ABSmartly web console
		Environment_: "prod", // created in the ABSmartly web console
	}

	var sdkConfig = sdk.ABSmartlyConfig{Client_: sdk.CreateDefaultClient(clientConfig)}

	var sd = sdk.Create(sdkConfig)

	var contextConfig = sdk.ContextConfig{
		Units_: map[string]string{ // add Unit
			"session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
			"user_id":    "123456", // a unique id identifying the user
		}}

	var ctx = sd.CreateContext(contextConfig)
	ctx.WaitUntilReady()

	//Creating a new Context with pre-fetched data
	var path, _ = os.Getwd()
	var content, _ = ioutil.ReadFile(path + "/sdk/testAssets/context.json")
	var deser = sdk.DefaultContextDataDeserializer{}
	var data, _ = deser.Deserialize(content)
	var anotherContextConfig = sdk.ContextConfig{
		Units_: map[string]string{
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
