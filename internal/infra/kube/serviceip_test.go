package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func svcWithIP(name, ip string) *corev1.Service {
	s := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "mc"}}
	if ip != "" {
		s.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: ip}}
	}
	return s
}

func TestServiceIPReadsTheAllocatedAddress(t *testing.T) {
	cs := fake.NewSimpleClientset(svcWithIP("mc-bob-03", "192.168.20.243"))
	c := NewWithClientset(cs, "mc", "mc-bob-03")

	got, err := c.ServiceIP(context.Background(), "mc-bob-03")
	if err != nil || got != "192.168.20.243" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPendingServiceReportsEmptyNotError(t *testing.T) {
	cs := fake.NewSimpleClientset(svcWithIP("mc-bob-03", ""))
	c := NewWithClientset(cs, "mc", "mc-bob-03")

	got, err := c.ServiceIP(context.Background(), "mc-bob-03")
	if err != nil {
		t.Fatalf("a pending LoadBalancer is a state, not an error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestServiceIPIsCached(t *testing.T) {
	cs := fake.NewSimpleClientset(svcWithIP("mc-bob-03", "192.168.20.243"))
	calls := 0
	cs.PrependReactor("get", "services", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		calls++
		return false, nil, nil
	})
	c := NewWithClientset(cs, "mc", "mc-bob-03")

	for range 25 {
		if _, err := c.ServiceIP(context.Background(), "mc-bob-03"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("hit the API %d times for 25 lookups: the dashboard polls several times a second and client-go throttles at 50 QPS", calls)
	}
}
