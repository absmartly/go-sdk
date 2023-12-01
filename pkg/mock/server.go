package mock

import (
	"context"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
)

const contextData = `{"experiments":[
{"id":32,"name":"brian_bulk_test","unitType":"device_id","iteration":1,"seedHi":12003383,"seedLo":-335999243,"split":[0.5,0.5],"trafficSeedHi":14699700,"trafficSeedLo":-1110414129,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":1,"name":"cross_sell_add_to_cat","unitType":"user_id","iteration":1,"seedHi":2939392,"seedLo":-1100806667,"split":[0.5,0.5],"trafficSeedHi":7024592,"trafficSeedLo":-226811223,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"Redirect to Suggestion Page","config":""},{"name":"Show Suggestion Panel","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":33,"name":"qa-automation-test","unitType":"device_id","iteration":1,"seedHi":10175854,"seedLo":-2081027442,"split":[0.5,0.5],"trafficSeedHi":4955332,"trafficSeedLo":-355935615,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"variant","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":3,"name":"another awesome experiment","unitType":"user_id","iteration":1,"seedHi":14977518,"seedLo":-1692045239,"split":[0.5,0.5],"trafficSeedHi":2916962,"trafficSeedLo":-1948611304,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":4,"name":"new header","unitType":"user_id","iteration":1,"seedHi":9740735,"seedLo":-1720102951,"split":[0.5,0.5],"trafficSeedHi":16349759,"trafficSeedLo":-1357658622,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":5,"name":"new footer","unitType":"user_id","iteration":1,"seedHi":13019988,"seedLo":-113148202,"split":[0.5,0.5],"trafficSeedHi":7228561,"trafficSeedLo":1991092269,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":37,"name":"dh-api-test","unitType":"user_id","iteration":4,"seedHi":4444864,"seedLo":572112513,"split":[0.05,0.9500000000000001],"trafficSeedHi":496851,"trafficSeedLo":1527949039,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"treatment","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":6,"name":"new design","unitType":"user_id","iteration":1,"seedHi":15356674,"seedLo":2091604149,"split":[0.5,0.5],"trafficSeedHi":12356140,"trafficSeedLo":-194164095,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":7,"name":"new cards","unitType":"user_id","iteration":1,"seedHi":12176527,"seedLo":-1589156502,"split":[0.5,0.5],"trafficSeedHi":14429623,"trafficSeedLo":-1086326567,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":8,"name":"my group sequential experiment","unitType":"user_id","iteration":2,"seedHi":10833035,"seedLo":-2035489974,"split":[0.5,0.5],"trafficSeedHi":3900234,"trafficSeedLo":1240167981,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":46,"name":"vr-test-2","unitType":"device_id","iteration":5,"seedHi":4248529,"seedLo":809639344,"split":[0.5,0.5],"trafficSeedHi":347629,"trafficSeedLo":-34417584,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"treatment","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":17,"name":"Consumer Credit Test A","unitType":"user_id","iteration":2,"seedHi":1525803,"seedLo":-567986281,"split":[0.5,0.5],"trafficSeedHi":10031949,"trafficSeedLo":364655298,"trafficSplit":[0.5,0.5],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":49,"name":"filter-test","unitType":"user_id","iteration":1,"seedHi":12067698,"seedLo":1477964936,"split":[0.5,0.5],"trafficSeedHi":1449277,"trafficSeedLo":-290243104,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"","config":""},{"name":"","config":""}],"audience":"{\"filter\":{\"and\":[{\"and\":[{\"eq\":[{\"value\":\"www\"},{\"var\":\"application\"}]},{\"eq\":[{\"value\":\"prod\"},{\"var\":\"environment\"}]}]},{\"or\":[{\"eq\":[{\"value\":\"US\"},{\"var\":\"country\"}]},{\"eq\":[{\"value\":\"en_US\"},{\"var\":\"language\"}]}]},{\"and\":[{\"gte\":[{\"value\":\"1\"},{\"var\":\"application_version\"}]},{\"lte\":[{\"value\":\"9\"},{\"var\":\"application_version\"}]}]}]}}","audienceStrict":true},
{"id":18,"name":"gc demo","unitType":"device_id","iteration":3,"seedHi":10006086,"seedLo":-578994615,"split":[0.5,0.5],"trafficSeedHi":6779123,"trafficSeedLo":352711291,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"treatment","config":""}],"audience":"{\"filter\":[{\"and\":[{\"or\":[{\"eq\":[{\"var\":{\"path\":\"agent\"}},{\"value\":\"test\"}]},{\"in\":[{\"var\":{\"path\":\"application\"}},{\"value\":\"www\"}]}]},{\"and\":[{\"eq\":[{\"var\":{\"path\":\"application_version\"}},{\"value\":\"1\"}]}]},{\"and\":[{\"eq\":[{\"var\":{\"path\":\"environment\"}},{\"value\":\"staging\"}]}]}]}]}","audienceStrict":true},
{"id":22,"name":"brian audience test","unitType":"device_id","iteration":1,"seedHi":15396533,"seedLo":1176790793,"split":[0.5,0.5],"trafficSeedHi":11085962,"trafficSeedLo":655434408,"trafficSplit":[0.0,1.0],"fullOnVariant":1,"applications":[{"name":"www"}],"variants":[{"name":"foo","config":""},{"name":"bar","config":""}],"audience":"{\"filter\":[{\"and\":[{\"and\":[{\"eq\":[{\"var\":{\"path\":\"user_agent\"}},{\"value\":\"test\"}]}]}]}]}","audienceStrict":true},
{"id":55,"name":"ccsh_spacing_test","unitType":"user_id","iteration":4,"seedHi":4332413,"seedLo":1352319717,"split":[0.334,0.33299999999999996,0.33299999999999996],"trafficSeedHi":10584564,"trafficSeedLo":950457776,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"},{"name":"cc-shopping-client"}],"variants":[{"name":"control","config":"{\"bg.color\":\"grey\"}"},{"name":"treatment1","config":"{\"bg.color\":\"blue\"}"},{"name":"treatment2","config":"{\"bg.color\":\"red\"}"}],"audience":"{\"filter\":[{\"and\":[{\"or\":[{\"in\":[{\"var\":{\"path\":\"url\"}},{\"value\":\"best/credit-cards\"}]}]}]}]}","audienceStrict":true},
{"id":56,"name":"Go SDK Experiment","unitType":"user_id","iteration":1,"seedHi":11231064,"seedLo":1050681462,"split":[0.5,0.5],"trafficSeedHi":3661487,"trafficSeedLo":1225345240,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"A","config":""},{"name":"B","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false},
{"id":28,"name":"dh test 2","unitType":"device_id","iteration":1,"seedHi":13076402,"seedLo":-1279429294,"split":[0.5,0.5],"trafficSeedHi":11405114,"trafficSeedLo":462805799,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"variant","config":""}],"audience":"{\"filter\":[{\"and\":[{\"and\":[{\"eq\":[{\"var\":{\"path\":\"name\"}},{\"value\":\"dan\"}]}]}]}]}","audienceStrict":false},
{"id":31,"name":"dh-local-test","unitType":"device_id","iteration":1,"seedHi":14941420,"seedLo":941196559,"split":[0.5,0.5],"trafficSeedHi":11284422,"trafficSeedLo":691465899,"trafficSplit":[0.0,1.0],"fullOnVariant":0,"applications":[{"name":"www"}],"variants":[{"name":"control","config":""},{"name":"variant","config":""}],"audience":"{\"filter\":[{\"and\":[]}]}","audienceStrict":false}]}`

type ServerMock struct {
	listen  string
	handler http.Handler
	cntGet  int32
	cntPut  int32
}

func NewServerMock(listen string) *ServerMock {
	router := mux.NewRouter()
	sm := &ServerMock{
		listen:  listen,
		handler: router,
	}
	router.HandleFunc("/context", sm.getContext).Methods(http.MethodGet)
	router.HandleFunc("/context", sm.putEvent).Methods(http.MethodPut)
	router.HandleFunc("/context/batch", sm.putEvent).Methods(http.MethodPut)
	return sm
}

func (sm *ServerMock) Run(ctx context.Context) error {
	srv := http.Server{
		Addr:    sm.listen,
		Handler: sm.handler,
	}
	go func() {
		<-ctx.Done()
		log.Printf(
			"Statistics: %d GET context, %d PUT events",
			atomic.LoadInt32(&sm.cntGet),
			atomic.LoadInt32(&sm.cntPut),
		)
		ctxTerm, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(ctxTerm)
		if err != nil {
			log.Println("Shutdown error:", err)
		}
	}()

	return srv.ListenAndServe()
}

func (sm *ServerMock) getContext(w http.ResponseWriter, r *http.Request) {
	_ = r.Body.Close()
	atomic.AddInt32(&sm.cntGet, 1)
	_, _ = w.Write([]byte(contextData))
}

func (sm *ServerMock) putEvent(w http.ResponseWriter, r *http.Request) {
	_ = r.Body.Close()
	atomic.AddInt32(&sm.cntPut, 1)
	w.WriteHeader(http.StatusOK)
}
