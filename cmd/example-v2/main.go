package main

import (
	"context"
	"log"
	"os"

	"github.com/absmartly/go-sdk/pkg/absmartly"
)

func main() {
	cfg := absmartly.Config{
		// Endpoint:        "https://demo-2.absmartly.io/v1",
		Endpoint:    "http://127.0.0.1:8123",
		ApiKey:      os.Getenv("ABSMARTLY_APIKEY"),
		Application: os.Getenv(`ABSMARTLY_APP`),
		Environment: os.Getenv(`ABSMARTLY_ENV`),
	}

	ctx := context.Background()
	sdk, err := absmartly.New(ctx, cfg)
	if err != nil {
		log.Println(err)
		return
	}

	uc := sdk.UnitContext(absmartly.Units{"user_id": "747"})

	treatment, err := uc.GetTreatment("Go SDK Experiment")
	log.Println("variant=", treatment)
	err = uc.Flush(ctx)
	log.Println("flush err=", err)

	treatment, ass, err := uc.PeekTreatment("Go SDK Experiment")
	log.Println("variant=", treatment, "ass=", ass, "err=", err)
	if ass != nil {
		err = uc.PushExposure(ctx, ass)
		log.Println("push err=", err)
	}
	err = uc.Flush(ctx)
	log.Println("flush err=", err)
}
