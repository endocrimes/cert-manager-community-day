package admission

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-hclog"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	jsonContentType = `application/json`
)

// PatchOperation is a JSON patch operation, see https://tools.ietf.org/html/rfc6902
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// AdmissionFunc is a callback for admission controller logic.
// Given an AdmissionRequest, it returns the sequence of patch operations to be
// applied before the object is admitted to Kubernetes, or the error that should
// be shown when the operation is rejected.
type AdmissionFunc func(logger hclog.Logger, request *v1beta1.AdmissionRequest) ([]PatchOperation, error)

// NamespaceAllowedFunc is called at the start of every admission request. If
// the function returns true then the request will be allowed.
// This allows you to easily ignore your own namespace, or kube system namespaces.
type NamespaceAllowedFunc func(ns string) bool

// Controller is a scaffold for a validating or mutating webhook. It is relatively
// lightweight but manages handling deserializing of admission requests and
// request/response validation.
type Controller struct {
	Logger       hclog.Logger
	Scheme       *runtime.Scheme
	Deserializer runtime.Decoder

	NamespaceAllowedFunc NamespaceAllowedFunc

	AdmissionFunc AdmissionFunc
}

type ControllerConfig struct {
	// See NamespaceAllowedFunc for documentation on the behaviour. If this function
	// is nil, we will exclude kube-system and kube-public.
	NamespaceAllowedFunc NamespaceAllowedFunc

	Logger hclog.Logger

	Scheme       *runtime.Scheme
	Deserializer runtime.Decoder

	AdmissionFunc AdmissionFunc
}

func (c *ControllerConfig) resolveDefaults() {
	if c.NamespaceAllowedFunc == nil {
		c.NamespaceAllowedFunc = isNotKubeNamespace
	}
}

func NewController(config *ControllerConfig) *Controller {
	config.resolveDefaults()
	return &Controller{
		Logger:               config.Logger.Named("admission-controller"),
		Scheme:               config.Scheme,
		Deserializer:         config.Deserializer,
		NamespaceAllowedFunc: config.NamespaceAllowedFunc,
		AdmissionFunc:        config.AdmissionFunc,
	}
}

func (c *Controller) handleAdmissionRequest(logger hclog.Logger, w http.ResponseWriter, r *http.Request) ([]byte, error) {
	// Step 1: Request validation (Valid requests are POST with Content-Type: application/json)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("invalid method %s", r.Method)
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("could not read request body: %v", err)
	}

	if contentType := r.Header.Get("Content-Type"); contentType != jsonContentType {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("invalid content type %s", contentType)
	}

	// Step 2: Parse the AdmissionReview request.
	var admissionReviewReq v1beta1.AdmissionReview
	if _, _, err := c.Deserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("could not deserialize request: %v", err)
	} else if admissionReviewReq.Request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, errors.New("malformed admission review: request is nil")
	}

	logger = logger.With("admission_id", admissionReviewReq.Request.UID)

	// Step 3: Construct the AdmissionReview response.
	admissionReviewResponse := v1beta1.AdmissionReview{
		Response: &v1beta1.AdmissionResponse{
			UID: admissionReviewReq.Request.UID,
		},
	}

	var patchOps []PatchOperation
	if c.NamespaceAllowedFunc(admissionReviewReq.Request.Namespace) {
		patchOps, err = c.AdmissionFunc(logger, admissionReviewReq.Request)
	} else {
		logger.Debug("Ignoring request due to disallowed namespace", "namespace", admissionReviewReq.Request.Namespace)
	}

	if err != nil {
		// If an error occured, we're going to deny the request and return the error
		admissionReviewResponse.Response.Allowed = false
		admissionReviewResponse.Response.Result = &metav1.Status{
			Message: err.Error(),
		}
	} else {
		// Otherwise we'll allow the request and return the patch operations.
		patchBytes, err := json.Marshal(patchOps)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return nil, fmt.Errorf("could not marshal JSON: %v", err)
		}
		admissionReviewResponse.Response.Allowed = true
		admissionReviewResponse.Response.Patch = patchBytes
	}

	// Return the AdmissionReview with a response as JSON.
	bytes, err := json.Marshal(&admissionReviewResponse)
	if err != nil {
		return nil, fmt.Errorf("could not marshall response: %v", err)
	}
	return bytes, nil
}

func (c *Controller) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	reqLogger := c.Logger.With("request_id", uuid.New())
	startTime := time.Now()

	var writeErr error
	if bytes, err := c.handleAdmissionRequest(reqLogger, w, r); err != nil {
		reqLogger.Error("error handling request", "error", err, "result", "error", "duration", time.Since(startTime))
		w.WriteHeader(http.StatusInternalServerError)
		_, writeErr = w.Write([]byte(err.Error()))
	} else {
		reqLogger.Info("handled request", "result", "success", "duration", time.Since(startTime))
		_, writeErr = w.Write(bytes)
	}

	if writeErr != nil {
		reqLogger.Error("failed to write response", "error", writeErr)
	}
}

func (c *Controller) HTTPHandlerFunc() http.Handler {
	return http.HandlerFunc(c.handleHTTPRequest)
}

// isNotKubeNamespace checks if the given namespace is a Kubernetes-owned namespace.
func isNotKubeNamespace(ns string) bool {
	return !(ns == metav1.NamespacePublic || ns == metav1.NamespaceSystem)
}
