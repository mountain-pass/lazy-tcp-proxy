package k8s

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cron "github.com/robfig/cron/v3"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

// Backend implements the containerBackend and backendManager interfaces using
// the Kubernetes API. Deployments are discovered by the label
// lazy-tcp-proxy.enabled=true; configuration is read from annotations.
type Backend struct {
	client       kubernetes.Interface
	namespace    string // "" = all namespaces
	mu           sync.RWMutex
	serviceNames map[string]string // targetID → Service name override
}

// NewBackend creates a Backend. It auto-detects config: in-cluster when running
// inside a pod, falling back to KUBECONFIG / ~/.kube/config for local use.
func NewBackend(namespace string) (*Backend, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules,
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: could not build config: %w", err)
		}
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: could not create clientset: %w", err)
	}
	return newBackendWithClient(clientset, namespace), nil
}

// newBackendWithClient constructs a Backend from an existing client (used in tests).
func newBackendWithClient(client kubernetes.Interface, namespace string) *Backend {
	return &Backend{
		client:       client,
		namespace:    namespace,
		serviceNames: make(map[string]string),
	}
}

// splitID splits a "namespace/name" targetID into its parts.
func splitID(targetID string) (namespace, name string) {
	parts := strings.SplitN(targetID, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "default", targetID
}

// targetID returns the canonical "namespace/name" identifier for a Deployment.
func targetID(d appsv1.Deployment) string {
	return d.Namespace + "/" + d.Name
}

// Discover lists all Deployments labelled lazy-tcp-proxy.enabled=true and
// calls handler.RegisterTarget for each valid one.
func (b *Backend) Discover(ctx context.Context, handler types.TargetHandler) error {
	list, err := b.client.AppsV1().Deployments(b.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "lazy-tcp-proxy.enabled=true",
	})
	if err != nil {
		return fmt.Errorf("k8s: list deployments: %w", err)
	}

	var found []string
	for _, d := range list.Items {
		info, err := b.deploymentToTargetInfo(d)
		if err != nil {
			log.Printf("k8s: discover: skipping %s/%s: %v", d.Namespace, d.Name, err)
			continue
		}
		b.storeServiceName(targetID(d), d.Annotations)
		handler.RegisterTarget(info)
		found = append(found, d.Name)
	}

	if len(found) == 0 {
		log.Printf("k8s: init: no proxy deployments found")
	} else {
		log.Printf("k8s: init: found deployments: \033[33m%s\033[0m", strings.Join(found, ", "))
	}
	return nil
}

// WatchEvents watches for Deployment label/annotation changes and calls the
// appropriate handler methods. Reconnects with exponential backoff on error.
func (b *Backend) WatchEvents(ctx context.Context, handler types.TargetHandler) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		watcher, err := b.client.AppsV1().Deployments(b.namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector: "lazy-tcp-proxy.enabled=true",
		})
		if err != nil {
			log.Printf("k8s: watch error: %v; retrying in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Printf("k8s: watching deployments...")

		for event := range watcher.ResultChan() {
			d, ok := event.Object.(*appsv1.Deployment)
			if !ok {
				continue
			}
			id := targetID(*d)
			switch event.Type {
			case watch.Added, watch.Modified:
				info, err := b.deploymentToTargetInfo(*d)
				if err != nil {
					log.Printf("k8s: event: skipping %s: %v", id, err)
					continue
				}
				b.storeServiceName(id, d.Annotations)
				handler.RegisterTarget(info)
				if info.Running {
					handler.ContainerStarted(id)
				} else {
					handler.ContainerStopped(id)
				}
				log.Printf("k8s: event: deployment updated: \033[33m%s\033[0m", d.Name)
			case watch.Deleted:
				log.Printf("k8s: event: deployment removed: \033[33m%s\033[0m", d.Name)
				handler.RemoveTarget(id)
			}
		}

		// ResultChan closed normally — reset backoff and reconnect promptly.
		// Do not increment backoff here: a normal channel close is not an error.
		select {
		case <-ctx.Done():
			return
		default:
			backoff = time.Second
			log.Printf("k8s: watch channel closed; reconnecting...")
			time.Sleep(backoff)
		}
	}
}

// EnsureRunning scales the Deployment to 1 replica if it is currently at 0.
// Readiness is handled by the proxy's existing dial-retry loop.
func (b *Backend) EnsureRunning(ctx context.Context, targetID string) error {
	ns, name := splitID(targetID)
	scale, err := b.client.AppsV1().Deployments(ns).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("k8s: get scale %s: %w", targetID, err)
	}
	if scale.Spec.Replicas > 0 {
		return nil // already running
	}
	log.Printf("k8s: scaling up deployment \033[33m%s\033[0m", name)
	scale.Spec.Replicas = 1
	if _, err := b.client.AppsV1().Deployments(ns).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("k8s: scale up %s: %w", targetID, err)
	}
	return nil
}

// StopContainer scales the Deployment to 0 replicas.
func (b *Backend) StopContainer(ctx context.Context, targetID, targetName string) error {
	ns, name := splitID(targetID)
	scale, err := b.client.AppsV1().Deployments(ns).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("k8s: get scale %s: %w", targetID, err)
	}
	if scale.Spec.Replicas == 0 {
		return nil // already stopped
	}
	log.Printf("k8s: scaling down deployment \033[33m%s\033[0m (idle timeout)", targetName)
	scale.Spec.Replicas = 0
	if _, err := b.client.AppsV1().Deployments(ns).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("k8s: scale down %s: %w", targetID, err)
	}
	log.Printf("k8s: deployment \033[33m%s\033[0m scaled to zero", targetName)
	return nil
}

// GetUpstreamHost returns the Kubernetes Service DNS name for the target.
// Defaults to Deployment name; overridden by the lazy-tcp-proxy.service-name annotation.
func (b *Backend) GetUpstreamHost(_ context.Context, tid, _ string) (string, error) {
	ns, name := splitID(tid)
	b.mu.RLock()
	svcName, ok := b.serviceNames[tid]
	b.mu.RUnlock()
	if !ok || svcName == "" {
		svcName = name
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, ns), nil
}

// Shutdown is a no-op for the k8s backend (no network joins to undo).
func (b *Backend) Shutdown(_ context.Context) {}

// DefaultTargetID returns "namespace/name" for use as a ContainerID when a
// YAML config entry has no matching discovered Deployment.
func (b *Backend) DefaultTargetID(name string) string {
	ns := b.namespace
	if ns == "" {
		ns = "default"
	}
	return ns + "/" + name
}

// WaitUntilHealthy is a no-op for the k8s backend. Kubernetes readiness is
// managed by the cluster via readiness probes; the proxy does not poll it.
func (b *Backend) WaitUntilHealthy(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}

// deploymentToTargetInfo converts a Deployment into a TargetInfo.
func (b *Backend) deploymentToTargetInfo(d appsv1.Deployment) (types.TargetInfo, error) {
	ann := d.Annotations
	if ann == nil {
		ann = map[string]string{}
	}

	portsStr := ann["lazy-tcp-proxy.ports"]
	udpPortsStr := ann["lazy-tcp-proxy.udp-ports"]
	if portsStr == "" && udpPortsStr == "" {
		return types.TargetInfo{}, fmt.Errorf("missing annotation lazy-tcp-proxy.ports or lazy-tcp-proxy.udp-ports")
	}
	var ports []types.PortMapping
	if portsStr != "" {
		ports = types.ParsePortMappings("lazy-tcp-proxy.ports", portsStr)
		if len(ports) == 0 {
			return types.TargetInfo{}, fmt.Errorf("annotation lazy-tcp-proxy.ports contains no valid port mappings")
		}
	}

	var udpPorts []types.PortMapping
	if udpPortsStr != "" {
		udpPorts = types.ParsePortMappings("lazy-tcp-proxy.udp-ports", udpPortsStr)
	}

	var allowList, blockList []net.IPNet
	if v := ann["lazy-tcp-proxy.allow-list"]; v != "" {
		allowList = types.ParseIPList("lazy-tcp-proxy.allow-list", v)
	}
	if v := ann["lazy-tcp-proxy.block-list"]; v != "" {
		blockList = types.ParseIPList("lazy-tcp-proxy.block-list", v)
	}

	idleTimeout := types.ParseIdleTimeoutLabel(d.Name, ann["lazy-tcp-proxy.idle-timeout-secs"])
	startTimeout := types.ParseStartTimeoutLabel(d.Name, ann["lazy-tcp-proxy.start-timeout-secs"])

	var webhookURL string
	if v := strings.TrimSpace(ann["lazy-tcp-proxy.webhook-url"]); v != "" {
		if _, err := url.ParseRequestURI(v); err != nil {
			log.Printf("k8s: deployment %s: ignoring invalid webhook URL %q: %v", d.Name, v, err)
		} else {
			webhookURL = v
		}
	}

	var dependants []string
	if v := strings.TrimSpace(ann["lazy-tcp-proxy.dependants"]); v != "" {
		dependants = types.ParseDependants(v)
	}

	cronStart := parseCronAnnotation(d.Name, "lazy-tcp-proxy.cron-start", ann["lazy-tcp-proxy.cron-start"])
	cronStop := parseCronAnnotation(d.Name, "lazy-tcp-proxy.cron-stop", ann["lazy-tcp-proxy.cron-stop"])

	httpHealthCheck := types.ParseHTTPHealthCheckLabel(d.Name, ann["lazy-tcp-proxy.http-healthcheck"])
	if httpHealthCheck != "" {
		// Log the configured URL, substituting ${container} with the Kubernetes
		// Service DNS name that GetUpstreamHost will use at connection time.
		svcName := d.Name
		if v := strings.TrimSpace(ann["lazy-tcp-proxy.service-name"]); v != "" {
			svcName = v
		}
		svcHost := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, d.Namespace)
		displayURL := strings.ReplaceAll(httpHealthCheck, "{{container}}", svcHost)
		log.Printf("%s: http-healthcheck URL configured %q", d.Name, displayURL)
	}

	return types.TargetInfo{
		ContainerID:     d.Namespace + "/" + d.Name,
		ContainerName:   d.Name,
		Ports:           ports,
		UDPPorts:        udpPorts,
		AllowList:       allowList,
		BlockList:       blockList,
		IdleTimeout:     idleTimeout,
		StartTimeout:    startTimeout,
		Running:         d.Status.ReadyReplicas > 0,
		WebhookURL:      webhookURL,
		Dependants:      dependants,
		CronStart:       cronStart,
		CronStop:        cronStop,
		HTTPHealthCheck: httpHealthCheck,
	}, nil
}

// parseCronAnnotation validates a cron expression from a Deployment annotation.
// Returns the expression unchanged if valid, or "" with a warning if invalid.
func parseCronAnnotation(deploymentName, annotationKey, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if _, err := cron.ParseStandard(v); err != nil {
		log.Printf("k8s: deployment %s: ignoring invalid %s %q: %v", deploymentName, annotationKey, v, err)
		return ""
	}
	return v
}

// storeServiceName caches the optional service-name annotation for a target.
func (b *Backend) storeServiceName(tid string, ann map[string]string) {
	svcName := ""
	if ann != nil {
		svcName = strings.TrimSpace(ann["lazy-tcp-proxy.service-name"])
	}
	b.mu.Lock()
	b.serviceNames[tid] = svcName
	b.mu.Unlock()
}

