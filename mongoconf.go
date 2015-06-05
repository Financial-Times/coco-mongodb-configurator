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
	if len(masters) != 1 {
		log.Fatal("replica set seems broken. exiting")
	}
	master := masters[0]
	addedSecondary := false
	for _, host := range hosts {
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
	return mi.IsMaster || mi.IsSecondary
}

func bootStrap(hosts []Host) {
	fmt.Println("initiating master")
	runMongo(hosts[0], "rs.initiate()")
	// we override our host & port here to ensure things work in a NAT environment.
	runMongo(hosts[0], fmt.Sprintf("var config = rs.config(); if (config.members.length === 1) { config.members[0].host = '%s:%d'; rs.reconfig(config); }", hosts[0].Hostname, hosts[0].Port))
	for _, slave := range hosts[1:] {
		addSecondary(hosts[0], slave)
	}
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
