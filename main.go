package main

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/endocrimes/cert-manager-community-day/pkg/admission"
	"github.com/hashicorp/go-hclog"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

var (
	scheme       *runtime.Scheme
	deserializer runtime.Decoder
	podGVR       = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

func init() {
	scheme = runtime.NewScheme()
	deserializer = serializer.NewCodecFactory(scheme).UniversalDeserializer()
}

func validatePod(logger hclog.Logger, req *v1beta1.AdmissionRequest) ([]admission.PatchOperation, error) {
	// This handler should only get called on Pod objects. If it's called for
	// anything else, log an error but otherwise admit the object to prevent broken
	// configuration from being unfixable.
	if req.Resource != podGVR {
		logger.Warn("Received an unexpected resource in handler", "resource_type", req.Resource)
		return nil, nil
	}

	// Parse the Pod.
	raw := req.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("could not deserialize pod: %v", err)
	}

	return nil, validatePodResourceLimits(pod)
}

func validatePodResourceLimits(pod corev1.Pod) error {
	for _, container := range pod.Spec.Containers {
		if container.Resources.Limits.Cpu().IsZero() {
			return fmt.Errorf("container (%s) is missing required CPU Limits", container.Name)
		}
		if container.Resources.Limits.Memory().IsZero() {
			return fmt.Errorf("container (%s) is missing required Memory Limits", container.Name)
		}
	}

	return nil
}

func main() {
	appLogger := hclog.New(&hclog.LoggerOptions{
		Name:       "cert-manager-demo",
		Level:      hclog.LevelFromString("DEBUG"),
		JSONFormat: true,
	})

	config := admission.ControllerConfig{
		NamespaceAllowedFunc: nil, // Use the default to ignore kube ns
		Logger:               appLogger,
		Scheme:               scheme,
		Deserializer:         deserializer,
		AdmissionFunc:        validatePod,
	}
	controller := admission.NewController(&config)

	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)
	mux := http.NewServeMux()
	mux.Handle("/my-admission-webhook", controller.HTTPHandlerFunc())
	server := &http.Server{
		Addr:    ":8443",
		Handler: mux,
	}
	err := server.ListenAndServeTLS(certPath, keyPath)
	if err != nil {
		appLogger.Error("Shutting down with an error", "error", err)
	}
}
