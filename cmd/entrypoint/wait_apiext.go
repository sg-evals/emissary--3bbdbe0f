package entrypoint

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/datawire/dlib/dlog"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/emissary-ingress/emissary/v3/pkg/kates"
)

// WaitForApiext waits for the emissary-apiext deployment to be ready before continuing.
// This ensures that CRD conversion webhooks are available before processing resources.
func WaitForApiext(ctx context.Context) error {
	// Check if waiting is enabled
	if !envbool("AMBASSADOR_APIEXT_WAIT") {
		dlog.Info(ctx, "Skipping apiext readiness check (AMBASSADOR_APIEXT_WAIT not set to true)")
		return nil
	}

	deploymentName := env("AMBASSADOR_APIEXT_DEPLOYMENT_NAME", "emissary-apiext")
	deploymentNamespace := env("AMBASSADOR_APIEXT_DEPLOYMENT_NAMESPACE", "emissary-system")
	timeoutSeconds := 300 // 5 minutes default

	if timeoutEnv := os.Getenv("AMBASSADOR_APIEXT_WAIT_TIMEOUT"); timeoutEnv != "" {
		if parsed, err := strconv.Atoi(timeoutEnv); err == nil && parsed > 0 {
			timeoutSeconds = parsed
		}
	}

	timeout := time.Duration(timeoutSeconds) * time.Second
	deadline := time.Now().Add(timeout)

	dlog.Infof(ctx, "Waiting for apiext deployment %s/%s to be ready (timeout: %v)",
		deploymentNamespace, deploymentName, timeout)

	// Create Kubernetes client
	client, err := kates.NewClient(kates.ClientConfig{})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	deployment := &appsv1.Deployment{
		TypeMeta: kates.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: kates.ObjectMeta{
			Name:      deploymentName,
			Namespace: deploymentNamespace,
		},
	}

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for apiext deployment %s/%s to be ready after %v",
				deploymentNamespace, deploymentName, timeout)
		}

		// Check if deployment exists
		err := client.Get(ctx, deployment, deployment)
		if err != nil {
			if errors.IsNotFound(err) {
				dlog.Infof(ctx, "Deployment %s/%s does not exist yet, waiting...",
					deploymentNamespace, deploymentName)
				time.Sleep(10 * time.Second)
				continue
			}
			return fmt.Errorf("error checking deployment %s/%s: %w",
				deploymentNamespace, deploymentName, err)
		}

		// Check if deployment is available
		if deployment.Status.Conditions != nil {
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == "True" {
					// Also check that replicas are stable (not in the middle of a rollout)
					desiredReplicas := int32(1)
					if deployment.Spec.Replicas != nil {
						desiredReplicas = *deployment.Spec.Replicas
					}

					currentReplicas := deployment.Status.Replicas
					if currentReplicas == desiredReplicas && deployment.Status.UpdatedReplicas == desiredReplicas {
						dlog.Infof(ctx, "Deployment %s/%s is ready and stable (replicas: %d/%d)",
							deploymentNamespace, deploymentName, currentReplicas, desiredReplicas)
						return nil
					}

					dlog.Infof(ctx, "Deployment %s/%s is available but replicas not stable yet (current: %d, desired: %d, updated: %d)",
						deploymentNamespace, deploymentName, currentReplicas, desiredReplicas, deployment.Status.UpdatedReplicas)
				}
			}
		}

		dlog.Debugf(ctx, "Deployment %s/%s not ready yet, waiting...", deploymentNamespace, deploymentName)
		time.Sleep(10 * time.Second)
	}
}
