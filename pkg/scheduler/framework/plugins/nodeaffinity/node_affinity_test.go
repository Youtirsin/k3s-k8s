/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodeaffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/internal/cache"
	st "k8s.io/kubernetes/pkg/scheduler/testing"
)

// TODO: Add test case for RequiredDuringSchedulingRequiredDuringExecution after it's implemented.
func TestNodeAffinity(t *testing.T) {
	tests := []struct {
		name                string
		pod                 *v1.Pod
		labels              map[string]string
		nodeName            string
		wantStatus          *framework.Status
		wantPreFilterStatus *framework.Status
		wantPreFilterResult *framework.PreFilterResult
		args                config.NodeAffinityArgs
		disablePreFilter    bool
	}{
		{
			name: "no selector",
			pod:  &v1.Pod{},
		},
		{
			name: "missing labels",
			pod: st.MakePod().NodeSelector(map[string]string{
				"foo": "bar",
			}).Obj(),
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "same labels",
			pod: st.MakePod().NodeSelector(map[string]string{
				"foo": "bar",
			}).Obj(),
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "node labels are superset",
			pod: st.MakePod().NodeSelector(map[string]string{
				"foo": "bar",
			}).Obj(),
			labels: map[string]string{
				"foo": "bar",
				"baz": "blah",
			},
		},
		{
			name: "node labels are subset",
			pod: st.MakePod().NodeSelector(map[string]string{
				"foo": "bar",
				"baz": "blah",
			}).Obj(),
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with matchExpressions using In operator that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar", "value2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "Pod with matchExpressions using Gt operator that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "kernel-version",
												Operator: v1.NodeSelectorOpGt,
												Values:   []string{"0204"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				// We use two digit to denote major version and two digit for minor version.
				"kernel-version": "0206",
			},
		},
		{
			name: "Pod with matchExpressions using NotIn operator that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "mem-type",
												Operator: v1.NodeSelectorOpNotIn,
												Values:   []string{"DDR", "DDR2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"mem-type": "DDR3",
			},
		},
		{
			name: "Pod with matchExpressions using Exists operator that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "GPU",
												Operator: v1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"GPU": "NVIDIA-GRID-K1",
			},
		},
		{
			name: "Pod with affinity that don't match node's labels won't schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"value1", "value2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with a nil []NodeSelectorTerm in affinity, can't match the node's labels and won't schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: nil,
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with an empty []NodeSelectorTerm in affinity, can't match the node's labels and won't schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with empty MatchExpressions is not a valid value will match no objects and won't schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with no Affinity will schedule onto a node",
			pod:  &v1.Pod{},
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "Pod with Affinity but nil NodeSelector will schedule onto a node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: nil,
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "Pod with multiple matchExpressions ANDed that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "GPU",
												Operator: v1.NodeSelectorOpExists,
											}, {
												Key:      "GPU",
												Operator: v1.NodeSelectorOpNotIn,
												Values:   []string{"AMD", "INTER"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"GPU": "NVIDIA-GRID-K1",
			},
		},
		{
			name: "Pod with multiple matchExpressions ANDed that doesn't match the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "GPU",
												Operator: v1.NodeSelectorOpExists,
											}, {
												Key:      "GPU",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"AMD", "INTER"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"GPU": "NVIDIA-GRID-K1",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with multiple NodeSelectorTerms ORed in affinity, matches the node's labels and will schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar", "value2"},
											},
										},
									},
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "diffkey",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"wrong", "value2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "Pod with an Affinity and a PodSpec.NodeSelector(the old thing that we are deprecating) " +
				"both are satisfied, will schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{
						"foo": "bar",
					},
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name: "Pod with an Affinity matches node's labels but the PodSpec.NodeSelector(the old thing that we are deprecating) " +
				"is not satisfied, won't schedule onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{
						"foo": "bar",
					},
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "barrrrrr",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with an invalid value in Affinity term won't be scheduled onto the node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpNotIn,
												Values:   []string{"invalid value: ___@#$%^"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with matchFields using In operator that matches the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node1",
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("node1")},
		},
		{
			name: "Pod with matchFields using In operator that does not match the existing node",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node2",
			wantStatus:          framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("node1")},
		},
		{
			name: "Pod with two terms: matchFields does not match, but matchExpressions matches",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
									},
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName: "node2",
			labels:   map[string]string{"foo": "bar"},
		},
		{
			name: "Pod with one term: matchFields does not match, but matchExpressions matches",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node2",
			labels:              map[string]string{"foo": "bar"},
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("node1")},
			wantStatus:          framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with one term: both matchFields and matchExpressions match",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node1",
			labels:              map[string]string{"foo": "bar"},
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("node1")},
		},
		{
			name: "Pod with two terms: both matchFields and matchExpressions do not match",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
									},
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "foo",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"not-match-to-bar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:   "node2",
			labels:     map[string]string{"foo": "bar"},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Pod with two terms of node.Name affinity",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
										},
									},
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node2",
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("node1", "node2")},
		},
		{
			name: "Pod with two conflicting mach field requirements",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchFields: []v1.NodeSelectorRequirement{
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node1"},
											},
											{
												Key:      metav1.ObjectNameField,
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"node2"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName:            "node2",
			labels:              map[string]string{"foo": "bar"},
			wantPreFilterStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, errReasonConflict),
			wantStatus:          framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name: "Matches added affinity and Pod's node affinity",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "zone",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"foo"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName: "node2",
			labels:   map[string]string{"zone": "foo"},
			args: config.NodeAffinityArgs{
				AddedAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{{
							MatchFields: []v1.NodeSelectorRequirement{{
								Key:      metav1.ObjectNameField,
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"node2"},
							}},
						}},
					},
				},
			},
		},
		{
			name: "Matches added affinity but not Pod's node affinity",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "zone",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"bar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			nodeName: "node2",
			labels:   map[string]string{"zone": "foo"},
			args: config.NodeAffinityArgs{
				AddedAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{{
							MatchFields: []v1.NodeSelectorRequirement{{
								Key:      metav1.ObjectNameField,
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"node2"},
							}},
						}},
					},
				},
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrReasonPod),
		},
		{
			name:     "Doesn't match added affinity",
			pod:      &v1.Pod{},
			nodeName: "node2",
			labels:   map[string]string{"zone": "foo"},
			args: config.NodeAffinityArgs{
				AddedAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "zone",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"bar"},
								},
							},
						}},
					},
				},
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, errReasonEnforced),
		},
		{
			name: "Matches node selector correctly even if PreFilter is not called",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{
						"foo": "bar",
					},
				},
			},
			labels: map[string]string{
				"foo": "bar",
				"baz": "blah",
			},
			disablePreFilter: true,
		},
		{
			name: "Matches node affinity correctly even if PreFilter is not called",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
								NodeSelectorTerms: []v1.NodeSelectorTerm{
									{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "GPU",
												Operator: v1.NodeSelectorOpExists,
											}, {
												Key:      "GPU",
												Operator: v1.NodeSelectorOpNotIn,
												Values:   []string{"AMD", "INTER"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"GPU": "NVIDIA-GRID-K1",
			},
			disablePreFilter: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := v1.Node{ObjectMeta: metav1.ObjectMeta{
				Name:   test.nodeName,
				Labels: test.labels,
			}}
			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(&node)

			p, err := New(&test.args, nil)
			if err != nil {
				t.Fatalf("Creating plugin: %v", err)
			}

			state := framework.NewCycleState()
			var gotStatus *framework.Status
			if !test.disablePreFilter {
				gotPreFilterResult, gotStatus := p.(framework.PreFilterPlugin).PreFilter(context.Background(), state, test.pod)
				if diff := cmp.Diff(test.wantPreFilterStatus, gotStatus); diff != "" {
					t.Errorf("unexpected PreFilter Status (-want,+got):\n%s", diff)
				}
				if diff := cmp.Diff(test.wantPreFilterResult, gotPreFilterResult); diff != "" {
					t.Errorf("unexpected PreFilterResult (-want,+got):\n%s", diff)
				}
			}
			gotStatus = p.(framework.FilterPlugin).Filter(context.Background(), state, test.pod, nodeInfo)
			if diff := cmp.Diff(test.wantStatus, gotStatus); diff != "" {
				t.Errorf("unexpected Filter Status (-want,+got):\n%s", diff)
			}
		})
	}
}

func TestNodeAffinityPriority(t *testing.T) {
	label1 := map[string]string{"foo": "bar"}
	label2 := map[string]string{"key": "value"}
	label3 := map[string]string{"az": "az1"}
	label4 := map[string]string{"abc": "az11", "def": "az22"}
	label5 := map[string]string{"foo": "bar", "key": "value", "az": "az1"}

	affinity1 := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{{
				Weight: 2,
				Preference: v1.NodeSelectorTerm{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      "foo",
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"bar"},
					}},
				},
			}},
		},
	}

	affinity2 := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
				{
					Weight: 2,
					Preference: v1.NodeSelectorTerm{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "foo",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"bar"},
							},
						},
					},
				},
				{
					Weight: 4,
					Preference: v1.NodeSelectorTerm{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "key",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"value"},
							},
						},
					},
				},
				{
					Weight: 5,
					Preference: v1.NodeSelectorTerm{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "foo",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"bar"},
							},
							{
								Key:      "key",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"value"},
							},
							{
								Key:      "az",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"az1"},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name            string
		pod             *v1.Pod
		nodes           []*v1.Node
		expectedList    framework.NodeScoreList
		args            config.NodeAffinityArgs
		disablePreScore bool
	}{
		{
			name: "all nodes are same priority as NodeAffinity is nil",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: label3}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: 0}, {Name: "node2", Score: 0}, {Name: "node3", Score: 0}},
		},
		{
			name: "no node matches preferred scheduling requirements in NodeAffinity of pod so all nodes' priority is zero",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: affinity1,
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label4}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: label3}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: 0}, {Name: "node2", Score: 0}, {Name: "node3", Score: 0}},
		},
		{
			name: "only node1 matches the preferred scheduling requirements of pod",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: affinity1,
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: label3}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: framework.MaxNodeScore}, {Name: "node2", Score: 0}, {Name: "node3", Score: 0}},
		},
		{
			name: "all nodes matches the preferred scheduling requirements of pod but with different priorities ",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: affinity2,
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node5", Labels: label5}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: 18}, {Name: "node5", Score: framework.MaxNodeScore}, {Name: "node2", Score: 36}},
		},
		{
			name: "added affinity",
			pod:  &v1.Pod{},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: framework.MaxNodeScore}, {Name: "node2", Score: 0}},
			args: config.NodeAffinityArgs{
				AddedAffinity: affinity1.NodeAffinity,
			},
		},
		{
			name: "added affinity and pod has default affinity",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: affinity1,
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: label5}},
			},
			expectedList: []framework.NodeScore{{Name: "node1", Score: 40}, {Name: "node2", Score: 60}, {Name: "node3", Score: framework.MaxNodeScore}},
			args: config.NodeAffinityArgs{
				AddedAffinity: &v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
						{
							Weight: 3,
							Preference: v1.NodeSelectorTerm{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "key",
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{"value"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "calculate the priorities correctly even if PreScore is not called",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Affinity: affinity2,
				},
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: label1}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node5", Labels: label5}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: label2}},
			},
			expectedList:    []framework.NodeScore{{Name: "node1", Score: 18}, {Name: "node5", Score: framework.MaxNodeScore}, {Name: "node2", Score: 36}},
			disablePreScore: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			state := framework.NewCycleState()
			fh, _ := runtime.NewFramework(nil, nil, ctx.Done(), runtime.WithSnapshotSharedLister(cache.NewSnapshot(nil, test.nodes)))
			p, err := New(&test.args, fh)
			if err != nil {
				t.Fatalf("Creating plugin: %v", err)
			}
			var status *framework.Status
			if !test.disablePreScore {
				status = p.(framework.PreScorePlugin).PreScore(ctx, state, test.pod, test.nodes)
				if !status.IsSuccess() {
					t.Errorf("unexpected error: %v", status)
				}
			}
			var gotList framework.NodeScoreList
			for _, n := range test.nodes {
				nodeName := n.ObjectMeta.Name
				score, status := p.(framework.ScorePlugin).Score(ctx, state, test.pod, nodeName)
				if !status.IsSuccess() {
					t.Errorf("unexpected error: %v", status)
				}
				gotList = append(gotList, framework.NodeScore{Name: nodeName, Score: score})
			}

			status = p.(framework.ScorePlugin).ScoreExtensions().NormalizeScore(ctx, state, test.pod, gotList)
			if !status.IsSuccess() {
				t.Errorf("unexpected error: %v", status)
			}

			if diff := cmp.Diff(test.expectedList, gotList); diff != "" {
				t.Errorf("obtained scores (-want,+got):\n%s", diff)
			}
		})
	}
}