package main

import (
	"fmt"
	"time"

	"sofastack.io/sofa-mosn/pkg/protocol/rpc/sofarpc"
	"sofastack.io/sofa-mosn/test/lib"
	testlib_sofarpc "sofastack.io/sofa-mosn/test/lib/sofarpc"
)

/*
Cluster have two subsets, each subset have one host(upstream server)
upstream server in different subset expected receive different header and do different response.
different request will route to different upstream server, and want to receivee different response.(same as server send)
*/

const ConfigStrTmpl = `{
	"servers":[
                {
                        "default_log_path":"stdout",
                        "default_log_level": "FATAL",
                        "listeners":[
                                {
                                        "address":"127.0.0.1:2045",
                                        "bind_port": true,
                                        "log_path": "stdout",
                                        "log_level": "FATAL",
                                        "filter_chains": [{
                                                "filters": [
                                                        {
                                                                "type": "proxy",
                                                                "config": {
                                                                        "downstream_protocol": "SofaRpc",
                                                                        "upstream_protocol": "%s",
                                                                        "router_config_name":"router_to_mosn"
                                                                }
                                                        },
                                                        {
                                                                "type": "connection_manager",
                                                                "config": {
                                                                        "router_config_name":"router_to_mosn",
                                                                        "virtual_hosts":[{
                                                                                "name":"mosn_hosts",
                                                                                "domains": ["*"],
                                                                                "routers": [
                                                                                        {
                                                                                                 "match":{"headers":[{"name":"service","value":".*"}]},
                                                                                                 "route":{"cluster_name":"mosn_cluster"}
                                                                                        }
                                                                                ]
                                                                        }]
                                                                }
                                                        }
                                                ]
                                        }]
                                },
				{
                                        "address":"127.0.0.1:2046",
                                        "bind_port": true,
                                        "log_path": "stdout",
                                        "log_LEVEL": "FATAL",
                                        "filter_chains": [{
                                                "filters": [
                                                        {
                                                                "type": "proxy",
                                                                "config": {
                                                                        "downstream_protocol": "%s",
                                                                        "upstream_protocol": "SofaRpc",
                                                                        "router_config_name":"router_to_server"
                                                                }
                                                        },
                                                        {
                                                                "type": "connection_manager",
                                                                "config": {
                                                                        "router_config_name":"router_to_server",
                                                                        "virtual_hosts":[{
                                                                                "name":"server_hosts",
                                                                                "domains": ["*"],
                                                                                "routers": [
                                                                                        {
                                                                                                 "match":{"headers":[{"name":"service","value":"1.0"}]},
                                                                                                 "route":{
													"cluster_name":"server_cluster",
													"metadata_match": {
														"filter_metadata": {
															"mosn.lb": {
																"version":"1.0"
															}
														}
													}
												}
                                                                                        },
											{
												"match":{"headers":[{"name":"service","value":"2.0"}]},
												"route":{
													"cluster_name":"server_cluster",
													"metadata_match": {
														"filter_metadata": {
															"mosn.lb": {
																 "version":"2.0"
															}
														}
													}
												}
											}
                                                                                ]
                                                                        }]
                                                                }
                                                        }
                                                ]
                                        }]
                                }
                        ]
                }
        ],
        "cluster_manager":{
                "clusters":[
                        {
                                "name": "mosn_cluster",
                                "type": "SIMPLE",
                                "lb_type": "LB_RANDOM",
                                "hosts":[
                                        {"address":"127.0.0.1:2046"}
                                ]
                        },
                        {
                                "name": "server_cluster",
                                "type": "SIMPLE",
                                "lb_type": "LB_RANDOM",
				"lb_subset_config": {
					"subset_selectors": [
						["version"]
					]
				},
                                "hosts":[
                                        {
						"address":"127.0.0.1:8080",
						"metadata": {
							"filter_metadata": {
								"mosn.lb": {
									"version":"1.0"
								}
							}
						}
					},
					{
						"address":"127.0.0.1:8081",
						"metadata": {
							"filter_metadata": {
								 "mosn.lb": {
									 "version":"2.0"
							 	}
							}
						}
					}
                                ]
                        }
                ]
        }

}`

func main() {
	lib.Execute(TestSubsetConvert)
}

func TestSubsetConvert() bool {
	convertList := []string{
		"Http1",
		"Http2",
	}
	for _, proto := range convertList {
		fmt.Println("----- RUN boltv1 -> ", proto, " subset test")
		if !RunCase(proto) {
			return false
		}
		fmt.Println("----- PASS boltv1 -> ", proto, " subset test")
	}
	return true
}

func RunCase(protocolStr string) bool {
	// Init
	configStr := fmt.Sprintf(ConfigStrTmpl, protocolStr, protocolStr)
	mosn := lib.StartMosn(configStr)
	defer mosn.Stop()

	// Server Config
	// If request header contains service_version:1.0, server resposne success
	// If not, server response error (by default)
	srv1 := MakeServer("127.0.0.1:8080", "1.0")
	go srv1.Start()
	defer srv1.Close()
	// service 2.0
	srv2 := MakeServer("127.0.0.1:8081", "2.0")
	go srv2.Start()
	defer srv2.Close()
	// Wait Server Start
	time.Sleep(time.Second)

	clientAddr := "127.0.0.1:2045"

	// test client version 1.0
	clt1 := MakeClient(clientAddr, "1.0")
	// requesy and verify
	for i := 0; i < 5; i++ {
		if !clt1.SyncCall() {
			fmt.Printf("client 1.0  request %s is failed\n", protocolStr)
			return false
		}
	}
	// stats verify
	srv1Stats := srv1.ServerStats
	srv2Stats := srv2.ServerStats
	if !(srv1Stats.RequestStats() == 5 &&
		srv1Stats.ResponseStats()[sofarpc.RESPONSE_STATUS_SUCCESS] == 5 &&
		srv2Stats.RequestStats() == 0) {
		fmt.Println("servers request and response is not expected", srv1Stats.RequestStats(), srv2Stats.RequestStats())
		return false
	}
	// test client version 2.0
	clt2 := MakeClient(clientAddr, "2.0")
	// requesy and verify
	for i := 0; i < 5; i++ {
		if !clt2.SyncCall() {
			fmt.Printf("client 2.0  request %s is failed\n", protocolStr)
			return false
		}
	}
	if !(srv1Stats.RequestStats() == 5 &&
		srv1Stats.ResponseStats()[sofarpc.RESPONSE_STATUS_SUCCESS] == 5 &&
		srv2Stats.RequestStats() == 5 &&
		srv2Stats.ResponseStats()[sofarpc.RESPONSE_STATUS_SUCCESS] == 5) {
		fmt.Println("servers request and response is not expected", srv1Stats.RequestStats(), srv2Stats.RequestStats())
		return false
	}
	return true
}

// Make a mock server, accept header contains service version, response header contains message
func MakeServer(addr string, version string) *testlib_sofarpc.MockServer {
	srvConfig := &testlib_sofarpc.BoltV1Serve{
		Configs: []*testlib_sofarpc.BoltV1ReponseConfig{
			{
				ExpectedHeader: map[string]string{
					"service": version,
				},
				Builder: &testlib_sofarpc.BoltV1ResponseBuilder{
					Status: sofarpc.RESPONSE_STATUS_SUCCESS,
					Header: map[string]string{
						"message": version,
					},
				},
			},
		},
	}
	srv := testlib_sofarpc.NewMockServer(addr, srvConfig.Serve)
	return srv
}

// make a mock client, send request header contain version, and want to response header contain message
func MakeClient(addr string, version string) *testlib_sofarpc.Client {
	cltVerify := &testlib_sofarpc.VerifyConfig{
		ExpectedStatus: sofarpc.RESPONSE_STATUS_SUCCESS,
		ExpectedHeader: map[string]string{
			"message": version,
		},
	}
	cltConfig := &testlib_sofarpc.ClientConfig{
		Addr:        addr,
		MakeRequest: testlib_sofarpc.BuildBoltV1Request,
		RequestHeader: map[string]string{
			"service": version,
		},
		Verify: cltVerify.Verify,
	}
	clt := testlib_sofarpc.NewClient(cltConfig, 1)
	return clt
}