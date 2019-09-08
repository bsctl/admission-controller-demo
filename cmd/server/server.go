// Copyright 2019 Clastix Tech Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Version is the current version
const Version string = "0.0.1"

func main() {

	version := flag.Bool("version", false, "Prints the current version")
	configDir := flag.String("confdir", "/opt/config", "Location of configuration file")
	configFile := flag.String("conf", "rules.json", "Configuration file")
	tlsDir := flag.String("tlsdir", "/opt/certs", "Location of TLS certificates")
	tlsCertFile := flag.String("cert", "tls.crt", "TLS certificate file")
	tlsKeyFile := flag.String("key", "tls.key", "TLS key file")
	ListenAddress := flag.String("addr", ":8443", "WebHook Server listen address")
	debugMode := flag.Bool("debug", false, "Enable debug mode")

	flag.Parse()

	// If version is specified, print and exit
	if *version {
		log.Printf("Clastix Admission Controller %s\n", Version)
		os.Exit(0)
	}

	certPath := filepath.Join(*tlsDir, *tlsCertFile)
	keyPath := filepath.Join(*tlsDir, *tlsKeyFile)
	configPath := filepath.Join(*configDir, *configFile)

	// Load the configuration
	config := &Config{}
	if err := config.LoadConfig(configPath); err != nil {
		log.Fatal("Failed to load rules: ", err)
	}

	// Load the TLS certificates
	certs, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal("Failed to load TLS: ", err)
	}

	// Create the Mux
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", AdmissionHandler)

	// Initialize the WebHook
	WebHook = &MutateWebHook{
		Config: config,
		Server: &http.Server{
			Addr:      *ListenAddress,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs}},
			Handler:   mux,
		},
		DebugMode: *debugMode,
	}

	// Start the WebHook in a new goroutine
	go func() {
		log.Printf("Clastix Admission Controller %s serving on %s", Version, WebHook.Server.Addr)
		if WebHook.DebugMode {
			log.Println("Warning: debug mode is not yet implemented")
		}
		err = WebHook.Server.ListenAndServeTLS("", "")
		log.Fatal(err)
	}()

	// Listening OS shutdown signal
	finish := make(chan os.Signal, 1)
	signal.Notify(finish, syscall.SIGINT, syscall.SIGTERM)
	<-finish

	log.Println("Shutting down gracefully")
	WebHook.Server.Shutdown(context.Background())

}

// LoadConfig is an helper function which parses from a configuration file.
func (conf *Config) LoadConfig(path string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(data), &conf)
	if err != nil {
		return err
	}
	return nil
}

// CheckCertificates is a helper function which checks if valid TLS certificates are provided
func CheckCertificates(certPath string, keyPath string) error {
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return err
	} else if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return err
	}
	return nil
}
