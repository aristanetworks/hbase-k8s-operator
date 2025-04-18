/*
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

package controller

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hbasev1 "github.com/timoha/hbase-k8s-operator/api/v1"
	//+kubebuilder:scaffold:imports
)

func makeHBaseSpec(confData map[string]string) *hbasev1.HBase {
	return &hbasev1.HBase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hbase",
			Namespace: "default",
		},
		Spec: hbasev1.HBaseSpec{
			RegionServerSpec: hbasev1.ServerSpec{
				Count: 3,
				Metadata: hbasev1.ServerMetadata{
					Labels: map[string]string{
						"hbase": "regionserver",
					},
				},
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "server",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/hbase/conf/hbase-site.xml",
									SubPath:   "hbase-site.xml",
								},
							},
						},
					},
				},
			},
			MasterSpec: hbasev1.ServerSpec{
				Count: 2,
				Metadata: hbasev1.ServerMetadata{
					Labels: map[string]string{
						"hbase": "master",
					},
				},
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "server",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/hbase/conf/hbase-site.xml",
									SubPath:   "hbase-site.xml",
								},
							},
						},
					},
				},
			},
			Config: hbasev1.ConfigMap{
				Data: confData,
			},
		},
	}
}

var _ = Describe("HBase controller", func() {
	var (
		timeout   = time.Second * 10
		interval  = time.Second * 1
		namespace = "default"
		ctx       = context.Background()
	)

	Context("When deploying HBase CRD", func() {
		It("Should deploy all resources", func() {
			By("By creating a new HBase")
			hb := makeHBaseSpec(map[string]string{"hbase-site.xml": "conf"})
			Expect(k8sClient.Create(ctx, hb)).Should(Succeed())
			hbaseLookupKey := types.NamespacedName{Name: "hbase", Namespace: namespace}
			createdHBase := &hbasev1.HBase{}

			Eventually(func() error {
				return k8sClient.Get(ctx, hbaseLookupKey, createdHBase)
			}, timeout, interval).Should(Succeed())

			By("By checking HBase deployed hbase service")
			Eventually(func() error {
				createdService := &corev1.Service{}
				return k8sClient.Get(ctx, hbaseLookupKey, createdService)
			}, timeout, interval).Should(Succeed())

			var createdConfigMap *corev1.ConfigMap
			By("By checking HBase deployed one config map")
			Eventually(func() (int, error) {
				configMapList := &corev1.ConfigMapList{}
				listOpts := []client.ListOption{
					client.InNamespace(namespace),
					client.MatchingLabels(map[string]string{"config": "core"}),
				}
				if err := k8sClient.List(ctx, configMapList, listOpts...); err != nil {
					return 0, err
				}
				l := len(configMapList.Items)
				if l != 1 {
					return l, nil
				}
				createdConfigMap = &(configMapList.Items[0])
				return l, nil
			}, timeout, interval).Should(Equal(1))

			By("By checking HBase deployed master statefulset")

			getExistingSts := func(name string, ns string, sts *appsv1.StatefulSet) {
				Eventually(func() error {
					stsName := types.NamespacedName{Name: name, Namespace: ns}
					return k8sClient.Get(ctx, stsName, sts)
				}, timeout, interval).Should(Succeed())
			}

			createdMasterStatefulSet := &appsv1.StatefulSet{}
			getExistingSts("hbasemaster", namespace, createdMasterStatefulSet)

			By("By checking master statefulset has correct number of replicas")
			Ω(*createdMasterStatefulSet.Spec.Replicas).Should(Equal(int32(2)))

			By("By checking master statefulset has mounted correct confgmap")
			vs := createdMasterStatefulSet.Spec.Template.Spec.Volumes
			Ω(len(vs)).Should(Equal(1))

			Ω(vs[0]).Should(Equal(corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: ptr.To(int32(420)),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: createdConfigMap.Name,
						},
					},
				},
			}))

			By("By checking HBase deployed master statefulset has annotation")
			_, ok := createdMasterStatefulSet.Annotations["hbase-controller-revision"]
			Ω(ok).Should(BeTrue())

			By("By checking HBase deployed regionserver statefulset")
			createdRegionServerStatefulSet := &appsv1.StatefulSet{}
			getExistingSts("regionserver", namespace, createdRegionServerStatefulSet)

			By("By checking regionserver statefulset has correct number of replicas")
			Ω(*createdRegionServerStatefulSet.Spec.Replicas).Should(Equal(int32(3)))

			By("By checking regionserver statefulset has mounted correct confgmap")
			vs = createdRegionServerStatefulSet.Spec.Template.Spec.Volumes
			Ω(len(vs)).Should(Equal(1))

			Ω(vs[0]).Should(Equal(corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: ptr.To(int32(420)),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: createdConfigMap.Name,
						},
					},
				},
			}))

			By("By checking regionserver statefulset has annotation")
			_, ok = createdRegionServerStatefulSet.Annotations["hbase-controller-revision"]
			Ω(ok).Should(BeTrue())
		})
	})

	Context("When updating config of HBase CRD", func() {
		It("Should redeploy or update resources", func() {
			By("By updating HBase")

			hbaseLookupKey := types.NamespacedName{Name: "hbase", Namespace: namespace}
			hb := &hbasev1.HBase{}
			Eventually(func() error {
				return k8sClient.Get(ctx, hbaseLookupKey, hb)
			}, timeout, interval).Should(Succeed())

			getExistingSts := func(name string, sts *appsv1.StatefulSet) {
				Eventually(func() error {
					stsName := types.NamespacedName{Name: name, Namespace: namespace}
					return k8sClient.Get(ctx, stsName, sts)
				}, timeout, interval).Should(Succeed())
			}

			getExistingStsAnnotations := func() (string, string) {
				masterSts := &appsv1.StatefulSet{}
				getExistingSts("hbasemaster", masterSts)
				rsSts := &appsv1.StatefulSet{}
				getExistingSts("regionserver", rsSts)
				return masterSts.Annotations["hbase-controller-revision"], rsSts.Annotations["hbase-controller-revision"]
			}

			var oldConfigMaps []corev1.ConfigMap
			getExistingCm := func() {
				Eventually(func() (int, error) {
					configMapList := &corev1.ConfigMapList{}
					listOpts := []client.ListOption{
						client.InNamespace(namespace),
						client.MatchingLabels(map[string]string{"config": "core"}),
					}
					if err := k8sClient.List(ctx, configMapList, listOpts...); err != nil {
						return 0, err
					}
					l := len(configMapList.Items)
					if l != 1 {
						return l, nil
					}
					oldConfigMaps = configMapList.Items
					return l, nil
				}, timeout, interval).Should(Equal(1))
			}

			// --------------------------- TEST 1 ---------------------------
			// Setup new test vars
			getExistingCm()
			updatedMasterSts := &appsv1.StatefulSet{}
			updatedRsSts := &appsv1.StatefulSet{}
			oldMasterAnnotation, oldRsAnnotation := getExistingStsAnnotations()

			// Test Case:
			// Different cm than initial spec, but preserve replica counts as initial spec
			// to test for revision SHA change
			newHB := makeHBaseSpec(map[string]string{"hbase-site.xml": "conf2"})
			hb.Spec.Config = newHB.Spec.Config
			hb.Spec.MasterSpec.Count = 2
			hb.Spec.RegionServerSpec.Count = 3
			Expect(k8sClient.Update(ctx, hb)).Should(Succeed())

			By("By checking phase in status is ApplyingChanges")
			Eventually(func() bool {
				k8sClient.Get(ctx, hbaseLookupKey, hb)
				return hb.Status.Phase == hbasev1.HBaseApplyingChangesPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressUpdatingCM
			}, 2*time.Second, 1*time.Millisecond).Should(BeTrue())

			By("By checking HBase deployed new config map")
			Eventually(func() ([]corev1.ConfigMap, error) {
				configMapList := &corev1.ConfigMapList{}
				listOpts := []client.ListOption{
					client.InNamespace(namespace),
					client.MatchingLabels(map[string]string{"config": "core"}),
				}
				if err := k8sClient.List(ctx, configMapList, listOpts...); err != nil {
					return nil, err
				}
				return configMapList.Items, nil
			}, timeout, interval).ShouldNot(Equal(oldConfigMaps))

			By("By checking HBase updated master statefulset revision")
			Eventually(func() (string, error) {
				masterName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, masterName, updatedMasterSts); err != nil {
					return oldMasterAnnotation, err
				}
				masterAnnotation, ok := updatedMasterSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldMasterAnnotation, errors.New("no annotation")
				}
				return masterAnnotation, nil
			}, timeout, interval).ShouldNot(Equal(oldMasterAnnotation))

			By("By checking master statefulset has not updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedMasterSts); err != nil {
					return 0, err
				}
				return int(*updatedMasterSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(2))

			By("By checking HBase updated regionserver sts revision annotation")
			Eventually(func() (string, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return oldRsAnnotation, err
				}
				rsAnnotation, ok := updatedRsSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldRsAnnotation, errors.New("no annotation")
				}
				return rsAnnotation, nil
			}, timeout, interval).ShouldNot(Equal(oldRsAnnotation))

			By("By checking regionserver statefulset has not updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return 0, err
				}
				return int(*updatedRsSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(3))

			By("By checking phase in status is Reconciled")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, hbaseLookupKey, hb)
				if err != nil {
					return false
				}
				return hb.Status.Phase == hbasev1.HBaseReadyPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressReady
			}, timeout, interval).Should(BeTrue())

			// --------------------------- TEST 2 ---------------------------
			// Clear test vars
			getExistingCm()
			updatedMasterSts = &appsv1.StatefulSet{}
			updatedRsSts = &appsv1.StatefulSet{}
			oldMasterAnnotation, oldRsAnnotation = getExistingStsAnnotations()

			// Test Case:
			// No cm updated (or other conf) so SHA should remain the same.
			// Update counts to get new replica counts
			hb.Spec.MasterSpec.Count = 1
			hb.Spec.RegionServerSpec.Count = 5
			Expect(k8sClient.Update(ctx, hb)).Should(Succeed())

			By("By checking phase in status is ApplyingChanges")
			Eventually(func() bool {
				k8sClient.Get(ctx, hbaseLookupKey, hb)
				return hb.Status.Phase == hbasev1.HBaseApplyingChangesPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressUpdatingMasters
			}, 2*time.Second, 1*time.Millisecond).Should(BeTrue())

			By("By checking HBase configmap was not updated")
			Eventually(func() ([]corev1.ConfigMap, error) {
				configMapList := &corev1.ConfigMapList{}
				listOpts := []client.ListOption{
					client.InNamespace(namespace),
					client.MatchingLabels(map[string]string{"config": "core"}),
				}
				if err := k8sClient.List(ctx, configMapList, listOpts...); err != nil {
					return nil, err
				}
				return configMapList.Items, nil
			}, timeout, interval).Should(Equal(oldConfigMaps))

			By("By checking HBase master sts revision was not updated")
			Eventually(func() (string, error) {
				masterName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, masterName, updatedMasterSts); err != nil {
					return oldMasterAnnotation, err
				}
				masterAnnotation, ok := updatedMasterSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldMasterAnnotation, errors.New("no annotation")
				}
				return masterAnnotation, nil
			}, timeout, interval).Should(Equal(oldMasterAnnotation))

			By("By checking HBase master statefulset updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedMasterSts); err != nil {
					return 0, err
				}
				return int(*updatedMasterSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(1))

			By("By checking HBase regionserver sts revision was not updated")
			Eventually(func() (string, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return oldRsAnnotation, err
				}
				rsAnnotation, ok := updatedRsSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldRsAnnotation, errors.New("no annotation")
				}
				return rsAnnotation, nil
			}, timeout, interval).Should(Equal(oldRsAnnotation))

			By("By checking HBase regionserver sts updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return 0, err
				}
				return int(*updatedRsSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(5))

			By("By checking phase in status is Reconciled")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, hbaseLookupKey, hb)
				if err != nil {
					return false
				}
				return hb.Status.Phase == hbasev1.HBaseReadyPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressReady
			}, timeout, interval).Should(BeTrue())

			// --------------------------- TEST 3 ---------------------------
			// Clear test vars
			getExistingCm()
			updatedMasterSts = &appsv1.StatefulSet{}
			updatedRsSts = &appsv1.StatefulSet{}
			oldMasterAnnotation, oldRsAnnotation = getExistingStsAnnotations()

			// Test Case:
			// Update configmap and replica counts
			newHB = makeHBaseSpec(map[string]string{"hbase-site.xml": "conf3"})
			hb.Spec.Config = newHB.Spec.Config
			hb.Spec.MasterSpec.Count = 2
			hb.Spec.RegionServerSpec.Count = 3
			Expect(k8sClient.Update(ctx, hb)).Should(Succeed())

			By("By checking phase in status is ApplyingChanges")
			Eventually(func() bool {
				k8sClient.Get(ctx, hbaseLookupKey, hb)
				return hb.Status.Phase == hbasev1.HBaseApplyingChangesPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressUpdatingCM
			}, 2*time.Second, 1*time.Millisecond).Should(BeTrue())

			By("By checking HBase configmap is updated")
			Eventually(func() ([]corev1.ConfigMap, error) {
				configMapList := &corev1.ConfigMapList{}
				listOpts := []client.ListOption{
					client.InNamespace(namespace),
					client.MatchingLabels(map[string]string{"config": "core"}),
				}
				if err := k8sClient.List(ctx, configMapList, listOpts...); err != nil {
					return nil, err
				}
				return configMapList.Items, nil
			}, timeout, interval).ShouldNot(Equal(oldConfigMaps))

			By("By checking HBase master sts revision was updated")
			Eventually(func() (string, error) {
				masterName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, masterName, updatedMasterSts); err != nil {
					return oldMasterAnnotation, err
				}
				masterAnnotation, ok := updatedMasterSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldMasterAnnotation, errors.New("no annotation")
				}
				return masterAnnotation, nil
			}, timeout, interval).ShouldNot(Equal(oldMasterAnnotation))

			By("By checking HBase master statefulset updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "hbasemaster", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedMasterSts); err != nil {
					return 0, err
				}
				return int(*updatedMasterSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(2))

			By("By checking HBase regionserver sts revision was updated")
			Eventually(func() (string, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return oldRsAnnotation, err
				}
				rsAnnotation, ok := updatedRsSts.Annotations["hbase-controller-revision"]
				if !ok {
					return oldRsAnnotation, errors.New("no annotation")
				}
				return rsAnnotation, nil
			}, timeout, interval).ShouldNot(Equal(oldRsAnnotation))

			By("By checking HBase regionserver sts updated replicas")
			Eventually(func() (int, error) {
				rsName := types.NamespacedName{Name: "regionserver", Namespace: namespace}
				if err := k8sClient.Get(ctx, rsName, updatedRsSts); err != nil {
					return 0, err
				}
				return int(*updatedRsSts.Spec.Replicas), nil
			}, timeout, interval).Should(Equal(3))

			By("By checking phase in status is Reconciled")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, hbaseLookupKey, hb)
				if err != nil {
					return false
				}
				return hb.Status.Phase == hbasev1.HBaseReadyPhase &&
					hb.Status.ReconcileProgress == hbasev1.HBaseProgressReady
			}, timeout, interval).Should(BeTrue())

		})
	})
})
