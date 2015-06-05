package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type MasterInfo struct {
	Info                string  `json:"info"`
	IsMaster            bool    `json:"ismaster"`
	IsSecondary         bool    `json:"secondary"`
	Isreplicaset        bool    `json:"isreplicaset"`
	MaxBsonObjectSize   float64 `json:"maxBsonObjectSize"`
	MaxMessageSizeBytes float64 `json:"maxMessageSizeBytes"`
	MaxWireVersion      float64 `json:"maxWireVersion"`
	MaxWriteBatchSize   float64 `json:"maxWriteBatchSize"`
	MinWireVersion      float64 `json:"minWireVersion"`
	Ok                  int     `json:"ok"`
}

type Host struct {
	Hostname  string
	Port      int
	Adminport int
}

func main() {
	var mongoHosts []Host
	flag.Parse()
	for _, input := range flag.Args() {
		parts := strings.Split(input, ":")
		if len(parts) != 3 {
			log.Fatalf("can't parse host details: %v\n", input)
		}

		port, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Fatalf("can't parse port: %v\n", parts[1])
		}
		adminport, err := strconv.Atoi(parts[2])
		if err != nil {
			log.Fatalf("can't parse port: %v\n", parts[2])
		}
		mongoHosts = append(mongoHosts, Host{parts[0], port, adminport})
	}

	fmt.Printf("hosts:%v\n", mongoHosts)
	configure(mongoHosts)
}

func configure(hosts []Host) {
	if len(hosts) == 0 {
		fmt.Println("no mongodb hosts to configure. exiting")
		return
	}

	if !anyConfigured(hosts) {
		bootStrap(hosts)
		return
	}

	masters := getMasters(hosts)

	if len(masters) == 1 {
		log.Printf("found master. checking for secondaries to add")
		master := masters[0]
		addedSecondary := false
		for _, host := range hosts {
			if !addedSecondary {
				// ensure we do this once per run. sometimes it has become "localhost" which causes pain
				fixSelfHostPort(master)
			}
			if host != master {
				mi := masterInfo(host)
				if !mi.IsMaster && !mi.IsSecondary {
					addSecondary(master, host)
					addedSecondary = true
				}
			}
		}
		if !addedSecondary {
			log.Printf("no new secondaries added")
		}
		return
	}

	if len(masters) > 1 {
		log.Printf("found %d masters. not good news. not trying to fix")
		return
	}

	if len(masters) == 0 && allRemoved(hosts) {
		log.Println("all instances are REMOVED. let's try to hook them back up again")
		fixAllRemoved(hosts)
		return
	}

	log.Fatal("replica set seems broken. exiting")

}

func anyConfigured(hosts []Host) bool {
	for _, host := range hosts {
		if configured(host) {
			return true
		}
	}
	return false
}

func configured(host Host) bool {
	mi := masterInfo(host)
	return mi.IsMaster || mi.IsSecondary || removed(host)
}

func allRemoved(hosts []Host) bool {
	for _, host := range hosts {
		if !removed(host) {
			return false
		}
	}
	return true
}

func removed(host Host) bool {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/replSetGetStatus", host.Hostname, host.Adminport), nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	var response map[string]interface{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Fatal(err)
	}

	stateStr := response["stateStr"]
	removed := (stateStr == "REMOVED")
	//log.Printf("stateStr %v removed %v\n", stateStr, removed)

	return removed
}

func fixAllRemoved(hosts []Host) {
	// we override our host & port here to ensure things work in a NAT environment.
	members := make([]map[string]interface{}, len(hosts))
	for i, h := range hosts {
		members[i] = make(map[string]interface{})
		members[i]["_id"] = i
		members[i]["host"] = fmt.Sprintf("%s:%d", h.Hostname, h.Port)
	}
	j, err := json.Marshal(members)
	if err != nil {
		panic(err)
	}
	fmt.Printf("j %s\n", j)
	fmt.Printf("forcing %v to be have replica set members %s\n", j)
	runMongo(hosts[0], fmt.Sprintf("var config = rs.config();  config.members=%s; rs.reconfig(config,{force:true}); ", j))
}

func bootStrap(hosts []Host) {
	fmt.Println("initiating master")
	runMongo(hosts[0], "rs.initiate()")
	fixSelfHostPort(hosts[0])
	for _, slave := range hosts[1:] {
		addSecondary(hosts[0], slave)
	}
}

func fixSelfHostPort(host Host) {
	// we override our host & port here to ensure things work in a NAT environment.
	runMongo(host, fmt.Sprintf("var config = rs.config(); if (config.members.length === 1) { config.members[0].host = '%s:%d'; rs.reconfig(config); }", host.Hostname, host.Port))
}

func runMongo(host Host, command string) {
	cmd := exec.Command("mongo", fmt.Sprintf("%s:%d", host.Hostname, host.Port))
	cmd.Stdin = strings.NewReader(command)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	log.Println(out.String())
}

func addSecondary(primary, slave Host) {
	log.Printf("adding secondary %v:%d to primary %v:%d\n", slave.Hostname, slave.Port, primary.Hostname, primary.Port)
	runMongo(primary, fmt.Sprintf("rs.add(\"%s:%d\")", slave.Hostname, slave.Port))
}

func getMasters(hosts []Host) (masters []Host) {
	for _, host := range hosts {
		mi := masterInfo(host)
		if mi.IsMaster {
			masters = append(masters, host)
		}
	}
	return
}

var client = &http.Client{}

func masterInfo(host Host) (mi MasterInfo) {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/isMaster", host.Hostname, host.Adminport), nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(body, &mi)
	if err != nil {
		log.Fatal(err)
	}
	return
}
