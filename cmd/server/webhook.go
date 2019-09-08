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
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	admission "k8s.io/api/admission/v1beta1"
	apis "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// Namespaces to be ignored by WebHook
var ignored = []string{
	apis.NamespaceSystem,
	apis.NamespacePublic,
	apis.NamespaceDefault,
}

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// MutateWebHook is the Mutating WebHook Server type
type MutateWebHook struct {
	Config    *Config
	Server    *http.Server
	DebugMode bool
}

// Config is a set of rules
type Config struct {
	DefaultSelector string            `json:"defaultselector"`
	Rules           map[string]string `json:"rules"`
}

// WebHook is global for the package
var WebHook *MutateWebHook

// AdmissionHandler for webhook server
func AdmissionHandler(response http.ResponseWriter, request *http.Request) {

	// Step 1: Request Validation
	log.Println("Serving a request")

	// load the body of the request
	var body []byte
	if request.Body != nil {
		if data, err := ioutil.ReadAll(request.Body); err == nil {
			body = data
		}
	}

	// verify no empty body
	if len(body) == 0 {
		http.Error(response, "Empty Body", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := request.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(response, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// Step2: Parse the Requested Admission Review
	requestedAdmissionReview := admission.AdmissionReview{}
	responseAdmissionReview := admission.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &requestedAdmissionReview); err != nil {
		responseAdmissionReview.Response = errorAdmissionResponse(err)
	}

	// Step 3: Construct the Admission Review Response
	reviewResponse, err := mutateResource(requestedAdmissionReview.Request)

	if err != nil {
		responseAdmissionReview.Response = errorAdmissionResponse(err)
	} else {
		responseAdmissionReview.Response = reviewResponse
	}
	// the UID in response must be the same of the requested
	responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID

	// Step 4: Return the Admission Review Response to the APIs Server
	data, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		http.Error(response, "Getting error during marshaling", http.StatusBadRequest)
		return
	}
	if _, err := response.Write(data); err != nil {
		http.Error(response, "Getting error during writing the response", http.StatusBadRequest)
		return
	}
}

// errorAdmissionResponse is a helper function to create an AdmissionResponse with an embedded error
func errorAdmissionResponse(err error) *admission.AdmissionResponse {
	return &admission.AdmissionResponse{
		Allowed: false,
		Result: &apis.Status{
			Code:    403,
			Message: err.Error(),
		},
	}
}

// mutationRequired is a helper function to check whether the target resource need to be mutated
func mutationIsRequired(ignoredList []string, namespace string) bool {
	for _, value := range ignoredList {
		if namespace == value {
			return false
		}
	}
	return true
}

// findSelector is the function which finds the node selector by namespace
func findSelector(namespace string) string {
	log.Println("Mutating for resource in namespace:", namespace)
	if selector, is := WebHook.Config.Rules[namespace]; is {
		return selector
	}
	return WebHook.Config.DefaultSelector
}

// patchBySelector is the function which patches the response
func patchBySelector(selector string) ([]patchOperation, error) {
	log.Println("Selector is:", selector)
	var patch []patchOperation
	labelsMap, err := labels.ConvertSelectorToLabelsMap(selector)
	if err != nil {
		return nil, err
	}
	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/spec/nodeSelector",
		Value: labelsMap,
	})
	return patch, nil
}

// mutateResource is the main function which mutates the resource
func mutateResource(reviewRequest *admission.AdmissionRequest) (*admission.AdmissionResponse, error) {

	// make sure only pods are mutated
	if reviewRequest.Resource.Resource != "pods" {
		return nil, errors.New("Expected mutating resource is pod")
	}
	namespace := reviewRequest.Namespace

	// check if mutation is really required
	if !mutationIsRequired(ignored, namespace) {
		log.Println("No mutation is required for namespace:", namespace)
		return &admission.AdmissionResponse{
			Allowed: true,
		}, nil
	}

	// patch by selector
	patch, err := patchBySelector(findSelector(namespace))
	if err != nil {
		return nil, err
	}

	// code patch data
	var raw []byte
	raw, err = json.Marshal(patch)
	if err != nil {
		return nil, err
	}

	// and return
	return &admission.AdmissionResponse{
		Allowed: true,
		Patch:   raw,
	}, nil
}
