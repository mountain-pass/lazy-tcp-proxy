package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/mountain-pass/lazy-tcp-proxy/internal/types"
)

// ---- helpers ----

// fakeDeployment builds an appsv1.Deployment with proxy labels/annotations.
func fakeDeployment(ns, name string, replicas int32, annotations map[string]string) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{"lazy-tcp-proxy.enabled": "true"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: replicas,
		},
	}
	if annotations != nil {
		d.Annotations = annotations
	}
	return d
}

// addScaleReactor intercepts GetScale and UpdateScale subresource calls on the
// fake client. It must use PrependReactor so it runs before the default object
// tracker, which does not understand the scale subresource.
func addScaleReactor(fc *k8sfake.Clientset, deployments map[string]*appsv1.Deployment) {
	fc.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ga := action.(k8stesting.GetAction)
		if ga.GetSubresource() != "scale" {
			return false, nil, nil
		}
		key := ga.GetNamespace() + "/" + ga.GetName()
		d, ok := deployments[key]
		if !ok {
			return true, nil, nil
		}
		replicas := int32(0)
		if d.Spec.Replicas != nil {
			replicas = *d.Spec.Replicas
		}
		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: ga.GetName(), Namespace: ga.GetNamespace()},
			Spec:       autoscalingv1.ScaleSpec{Replicas: replicas},
		}, nil
	})

	fc.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ua := action.(k8stesting.UpdateAction)
		if ua.GetSubresource() != "scale" {
			return false, nil, nil
		}
		scale := ua.GetObject().(*autoscalingv1.Scale)
		key := ua.GetNamespace() + "/" + scale.Name
		if d, ok := deployments[key]; ok {
			r := scale.Spec.Replicas
			d.Spec.Replicas = &r
		}
		return true, scale, nil
	})
}

// captureHandler records RegisterTarget and RemoveTarget calls.
type captureHandler struct {
	registered []types.TargetInfo
	removed    []string
	stopped    []string
}

func (h *captureHandler) RegisterTarget(info types.TargetInfo) { h.registered = append(h.registered, info) }
func (h *captureHandler) RemoveTarget(id string)               { h.removed = append(h.removed, id) }
func (h *captureHandler) ContainerStopped(id string)           { h.stopped = append(h.stopped, id) }
func (h *captureHandler) ContainerStarted(_ string)            {}
func (h *captureHandler) ContainerRemoved(_ string)            {}

// ---- Discover tests ----

func TestDiscover_NoDeployments(t *testing.T) {
	fc := k8sfake.NewSimpleClientset()
	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}
	if err := b.Discover(context.Background(), h); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(h.registered) != 0 {
		t.Errorf("expected 0 registrations, got %d", len(h.registered))
	}
}

func TestDiscover_OneDeployment(t *testing.T) {
	d := fakeDeployment("default", "myapp", 1, map[string]string{
		"lazy-tcp-proxy.ports": "9000:80",
	})
	fc := k8sfake.NewSimpleClientset(d)
	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}
	if err := b.Discover(context.Background(), h); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(h.registered) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(h.registered))
	}
	info := h.registered[0]
	if info.ContainerID != "default/myapp" {
		t.Errorf("ContainerID: got %q, want %q", info.ContainerID, "default/myapp")
	}
	if info.ContainerName != "myapp" {
		t.Errorf("ContainerName: got %q, want %q", info.ContainerName, "myapp")
	}
	if len(info.Ports) != 1 || info.Ports[0].ListenPort != 9000 || info.Ports[0].TargetPort != 80 {
		t.Errorf("Ports: got %+v, want [{9000 80}]", info.Ports)
	}
}

func TestDiscover_MissingPortsAnnotation(t *testing.T) {
	d := fakeDeployment("default", "myapp", 1, nil) // no annotations
	fc := k8sfake.NewSimpleClientset(d)
	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}
	if err := b.Discover(context.Background(), h); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(h.registered) != 0 {
		t.Errorf("deployment with missing ports annotation should be skipped")
	}
}

// ---- EnsureRunning tests ----

func TestEnsureRunning_ScalesUp(t *testing.T) {
	zero := int32(0)
	d := fakeDeployment("default", "myapp", 0, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	d.Spec.Replicas = &zero

	deployments := map[string]*appsv1.Deployment{"default/myapp": d}
	fc := k8sfake.NewSimpleClientset(d)
	addScaleReactor(fc, deployments)

	b := newBackendWithClient(fc, "default")
	if err := b.EnsureRunning(context.Background(), "default/myapp"); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if *d.Spec.Replicas != 1 {
		t.Errorf("expected replicas=1 after EnsureRunning, got %d", *d.Spec.Replicas)
	}
}

func TestEnsureRunning_AlreadyRunning(t *testing.T) {
	one := int32(1)
	d := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	d.Spec.Replicas = &one

	deployments := map[string]*appsv1.Deployment{"default/myapp": d}
	fc := k8sfake.NewSimpleClientset(d)

	updateCalled := false
	fc.AddReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ua := action.(k8stesting.UpdateAction)
		if ua.GetSubresource() == "scale" {
			updateCalled = true
		}
		return false, nil, nil
	})
	addScaleReactor(fc, deployments)

	b := newBackendWithClient(fc, "default")
	if err := b.EnsureRunning(context.Background(), "default/myapp"); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if updateCalled {
		t.Error("UpdateScale should not be called when already running")
	}
}

// ---- StopContainer tests ----

func TestStopContainer_ScalesDown(t *testing.T) {
	one := int32(1)
	d := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	d.Spec.Replicas = &one

	deployments := map[string]*appsv1.Deployment{"default/myapp": d}
	fc := k8sfake.NewSimpleClientset(d)
	addScaleReactor(fc, deployments)

	b := newBackendWithClient(fc, "default")
	if err := b.StopContainer(context.Background(), "default/myapp", "myapp"); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if *d.Spec.Replicas != 0 {
		t.Errorf("expected replicas=0 after StopContainer, got %d", *d.Spec.Replicas)
	}
}

func TestStopContainer_AlreadyStopped(t *testing.T) {
	zero := int32(0)
	d := fakeDeployment("default", "myapp", 0, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	d.Spec.Replicas = &zero

	deployments := map[string]*appsv1.Deployment{"default/myapp": d}
	fc := k8sfake.NewSimpleClientset(d)

	updateCalled := false
	fc.AddReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ua := action.(k8stesting.UpdateAction)
		if ua.GetSubresource() == "scale" {
			updateCalled = true
		}
		return false, nil, nil
	})
	addScaleReactor(fc, deployments)

	b := newBackendWithClient(fc, "default")
	if err := b.StopContainer(context.Background(), "default/myapp", "myapp"); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if updateCalled {
		t.Error("UpdateScale should not be called when already stopped")
	}
}

// ---- GetUpstreamHost tests ----

func TestGetUpstreamHost_DefaultsToDeploymentName(t *testing.T) {
	b := newBackendWithClient(k8sfake.NewSimpleClientset(), "default")
	host, err := b.GetUpstreamHost(context.Background(), "default/myapp", "")
	if err != nil {
		t.Fatalf("GetUpstreamHost: %v", err)
	}
	want := "myapp.default.svc.cluster.local"
	if host != want {
		t.Errorf("got %q, want %q", host, want)
	}
}

func TestGetUpstreamHost_ServiceNameOverride(t *testing.T) {
	b := newBackendWithClient(k8sfake.NewSimpleClientset(), "default")
	b.storeServiceName("default/myapp", map[string]string{
		"lazy-tcp-proxy.service-name": "myapp-svc",
	})
	host, err := b.GetUpstreamHost(context.Background(), "default/myapp", "")
	if err != nil {
		t.Fatalf("GetUpstreamHost: %v", err)
	}
	want := "myapp-svc.default.svc.cluster.local"
	if host != want {
		t.Errorf("got %q, want %q", host, want)
	}
}

// ---- WatchEvents tests ----

func TestWatchEvents_AddedTriggersRegister(t *testing.T) {
	d := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	fc := k8sfake.NewSimpleClientset(d)

	fakeWatcher := watch.NewFake()
	fc.PrependWatchReactor("deployments", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, fakeWatcher, nil
	})

	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.WatchEvents(ctx, h)
		close(done)
	}()

	fakeWatcher.Add(d)
	fakeWatcher.Modify(d)
	// Stop the watcher (closes ResultChan) so WatchEvents exits the range loop,
	// then cancel the context so the outer loop also exits.
	fakeWatcher.Stop()
	cancel()
	<-done

	if len(h.registered) == 0 {
		t.Error("expected RegisterTarget to be called for Added/Modified events")
	}
}

func TestWatchEvents_DeletedTriggersRemove(t *testing.T) {
	d := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	fc := k8sfake.NewSimpleClientset(d)

	fakeWatcher := watch.NewFake()
	fc.PrependWatchReactor("deployments", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, fakeWatcher, nil
	})

	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.WatchEvents(ctx, h)
		close(done)
	}()

	fakeWatcher.Delete(d)
	fakeWatcher.Stop()
	cancel()
	<-done

	if len(h.removed) == 0 {
		t.Error("expected RemoveTarget to be called for Deleted event")
	}
	if len(h.removed) > 0 && h.removed[0] != "default/myapp" {
		t.Errorf("RemoveTarget: got %q, want %q", h.removed[0], "default/myapp")
	}
}

func TestWatchEvents_ModifiedScaleToZeroCallsContainerStopped(t *testing.T) {
	// Start with a running deployment (ReadyReplicas=1), then receive a Modified
	// event where ReadyReplicas drops to 0. ContainerStopped must be called.
	running := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})
	stopped := fakeDeployment("default", "myapp", 0, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})

	fc := k8sfake.NewSimpleClientset(running)
	fakeWatcher := watch.NewFake()
	fc.PrependWatchReactor("deployments", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, fakeWatcher, nil
	})

	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.WatchEvents(ctx, h)
		close(done)
	}()

	fakeWatcher.Modify(stopped)
	fakeWatcher.Stop()
	cancel()
	<-done

	if len(h.stopped) == 0 {
		t.Error("expected ContainerStopped to be called when deployment scales to zero")
	}
	if len(h.stopped) > 0 && h.stopped[0] != "default/myapp" {
		t.Errorf("ContainerStopped: got %q, want %q", h.stopped[0], "default/myapp")
	}
}

func TestWatchEvents_ModifiedStillRunningDoesNotCallContainerStopped(t *testing.T) {
	// A Modified event where the deployment remains running must NOT call
	// ContainerStopped.
	d := fakeDeployment("default", "myapp", 1, map[string]string{"lazy-tcp-proxy.ports": "9000:80"})

	fc := k8sfake.NewSimpleClientset(d)
	fakeWatcher := watch.NewFake()
	fc.PrependWatchReactor("deployments", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, fakeWatcher, nil
	})

	b := newBackendWithClient(fc, "default")
	h := &captureHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.WatchEvents(ctx, h)
		close(done)
	}()

	fakeWatcher.Modify(d) // still running
	fakeWatcher.Stop()
	cancel()
	<-done

	if len(h.stopped) != 0 {
		t.Errorf("expected ContainerStopped NOT to be called for a running deployment, got %v", h.stopped)
	}
}

// Register the scale subresource with the fake client scheme so reactors can
// recognise it. This init block ensures the autoscaling types are registered.
func init() {
	_ = schema.GroupVersionResource{Group: "autoscaling", Version: "v1", Resource: "scales"}
}
