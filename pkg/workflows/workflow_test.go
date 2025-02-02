/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package workflows

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/record"
	runtimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/keikoproj/addon-manager/api/v1alpha1"
	"github.com/keikoproj/addon-manager/pkg/common"
)

var sch = runtime.NewScheme()
var fclient = runtimefake.NewFakeClientWithScheme(sch)
var dynClient = dynfake.NewSimpleDynamicClient(sch)
var rcdr = record.NewBroadcasterForTests(1*time.Second).NewRecorder(sch, v1.EventSource{Component: "addons"})

var wfSpecTemplate = `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: scripts-python-
spec:
  entrypoint: python-script-example
  templates:
    - name: python-script-example
      steps:
        - - name: generate
            template: gen-random-int
        - - name: print
            template: print-message
            arguments:
              parameters:
                - name: message
                  value: "{{steps.generate.outputs.result}}"

    - name: gen-random-int
      script:
        image: python:alpine3.6
        command: [python]
        source: |
          import random
          i = random.randint(1, 100)
          print(i)
    - name: print-message
      inputs:
        parameters:
          - name: message
      container:
        image: alpine:latest
        command: [sh, -c]
        args: ["echo result was: {{inputs.parameters.message}}"]
`

func init() {
	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "Workflow",
		Group:   "argoproj.io",
		Version: "v1alpha1",
	})
	wfList := &unstructured.UnstructuredList{}
	wfList.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "WorkflowList",
		Group:   "argoproj.io",
		Version: "v1alpha1",
	})
	sch.AddKnownTypes(common.AddonGVR().GroupVersion(), &v1alpha1.Addon{}, &v1alpha1.AddonList{})
	sch.AddKnownTypes(common.WorkflowGVR().GroupVersion(), wf, wfList)
	metav1.AddToGroupVersion(sch, common.WorkflowGVR().GroupVersion())
}

func TestNewWorkflowLifecycle(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &v1alpha1.Addon{}

	wfl := NewWorkflowLifecycle(fclient, dynClient, a, rcdr, sch)

	var expected AddonLifecycle = &workflowLifecycle{}
	g.Expect(wfl).To(BeAssignableToTypeOf(expected))
}

func TestWorkflowLifecycle_Install(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &v1alpha1.Addon{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.AddonSpec{
			PackageSpec: v1alpha1.PackageSpec{
				PkgName:        "my-addon",
				PkgVersion:     "1.0.0",
				PkgType:        v1alpha1.HelmPkg,
				PkgDescription: "",
				PkgDeps:        map[string]string{"core/A": "*", "core/B": "v1.0.0"},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "my-app",
				},
			},
		},
	}

	wfl := NewWorkflowLifecycle(fclient, dynClient, a, rcdr, sch)

	wt := &v1alpha1.WorkflowType{
		NamePrefix: "test",
		Role:       "myrole",
		Template:   wfSpecTemplate,
	}

	phase, err := wfl.Install(context.Background(), wt, "addon-wf-test")

	g.Expect(err).To(Not(HaveOccurred()))
	g.Expect(phase).To(Equal(v1alpha1.Pending))
}

// Test that an empty workflow type will fail
func TestWorkflowLifecycle_Install_InvalidWorkflowType(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &v1alpha1.Addon{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.AddonSpec{
			PackageSpec: v1alpha1.PackageSpec{
				PkgName:        "my-addon",
				PkgVersion:     "1.0.0",
				PkgType:        v1alpha1.HelmPkg,
				PkgDescription: "",
				PkgDeps:        map[string]string{"core/A": "*", "core/B": "v1.0.0"},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "my-app",
				},
			},
		},
	}

	wfl := NewWorkflowLifecycle(fclient, dynClient, a, rcdr, sch)

	// Empty workflow type should fail
	wt := &v1alpha1.WorkflowType{}

	phase, err := wfl.Install(context.Background(), wt, "addon-wf-test")

	g.Expect(err).To(HaveOccurred())
	g.Expect(phase).To(Equal(v1alpha1.Failed))
}

func TestWorkflowLifecycle_Delete_NotExists(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &v1alpha1.Addon{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.AddonSpec{
			PackageSpec: v1alpha1.PackageSpec{
				PkgName:        "my-addon",
				PkgVersion:     "1.0.0",
				PkgType:        v1alpha1.HelmPkg,
				PkgDescription: "",
				PkgDeps:        map[string]string{"core/A": "*", "core/B": "v1.0.0"},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "my-app",
				},
			},
		},
	}

	wfl := NewWorkflowLifecycle(fclient, dynClient, a, rcdr, sch)

	g.Expect(wfl.Delete("addon-wf-test")).To(HaveOccurred())
}

func TestNewWorkflowLifecycle_Delete(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &v1alpha1.Addon{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: v1alpha1.AddonSpec{
			PackageSpec: v1alpha1.PackageSpec{
				PkgName:        "my-addon",
				PkgVersion:     "1.0.0",
				PkgType:        v1alpha1.HelmPkg,
				PkgDescription: "",
				PkgDeps:        map[string]string{"core/A": "*", "core/B": "v1.0.0"},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "my-app",
				},
			},
		},
	}

	wfl := NewWorkflowLifecycle(fclient, dynClient, a, rcdr, sch)

	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "Workflow",
		Group:   "argoproj.io",
		Version: "v1alpha1",
	})

	wf.SetNamespace("default")
	wf.SetName("addon-wf-test")

	_, err := dynClient.Resource(common.WorkflowGVR()).Namespace("default").Create(wf, metav1.CreateOptions{})
	g.Expect(err).To(Not(HaveOccurred()))

	// Now try to delete
	g.Expect(wfl.Delete("addon-wf-test")).To(Not(HaveOccurred()))
}
