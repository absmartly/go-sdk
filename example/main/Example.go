package main

import (
	"fmt"
	"github.com/absmartly/go-sdk/sdk"
	"os"
)

func main() {
	var clientConfig = sdk.ClientConfig{
		Endpoint_:    "https://acme.absmartly.io/v1",
		ApiKey_:      os.Getenv("ABSMARTLY_APIKEY"),
		Application_: os.Getenv("website"),     // created in the ABSmartly web console
		Environment_: os.Getenv("development"), // created in the ABSmartly web console
	}

	var sdkConfig = sdk.ABSmartlyConfig{Client_: sdk.CreateDefaultClient(clientConfig)}

	var sd = sdk.Create(sdkConfig)

	var contextConfig = sdk.ContextConfig{
		Units_: map[string]string{ // add Unit
			"session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
			"user_id":    "123456", // a unique id identifying the user
		}}

	//This is alternative to Java ThreadLocal buffers,
	//Go best practices is just passing  them to methods due to lack of ThreadLocal implementations
	var buff [512]byte
	var block [16]int32
	var st [4]int32
	var assignBuf [12]int8

	var ctx = sd.CreateContext(contextConfig, buff, block, st)
	ctx.WaitUntilReady()

	//Creating a new Context with pre-fetched data
	//var path, _ = os.Getwd()
	//var content, _ = ioutil.ReadFile(path + "/context.json")
	//var deser = sdk.DefaultContextDataDeserializer{}
	//var data, _ = deser.Deserialize(content)
	//var anotherContextConfig = sdk.ContextConfig{
	//	Units_: map[string]string{
	//		"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
	//		"user_id":    "123456789",
	//		"email":      "bleh@absmartly.com",
	//	}}
	//
	//var anotherCtx = sd.CreateContextWith(anotherContextConfig, data, buff, block, st)
	//fmt.Println(anotherCtx.IsReady())
	//fmt.Println(anotherCtx.GetTreatment("exp_test_fullon", buff, block, st, assignBuf))

	var treatment, _ = ctx.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	fmt.Println(treatment)

	var properties = map[string]interface{}{
		"value": 125,
		"fee":   125,
	}

	var err = ctx.Track("payment", properties, buff, block, st)

	fmt.Println(err)

	ctx.Close(buff, block, st)

}
