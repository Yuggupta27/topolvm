package e2e

import (
	_ "embed"
	"fmt"

	//	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	//go:embed testdata/cloning/clone-pod-template.yaml
	thinPodCloneTemplateYAML string

	//go:embed testdata/cloning/clone-pvc-template.yaml
	thinPvcCloneTemplateYAML string
)

const (
	nsCloneTest      = "clone-test"
	thinClonePVCName = "thin-clone"
)

func testPVCClone() {
	// testNamespace
	// var cc CleanupContext
	BeforeEach(func() {
		// cc = commonBeforeEach()
		createNamespace(nsCloneTest)
	})
	AfterEach(func() {
		if !CurrentGinkgoTestDescription().Failed {
			kubectl("delete", "namespaces/"+nsCloneTest)
		}
		//commonAfterEach(cc)
	})

	It("should create a PVC Clone", func() {
		By("deploying Pod with PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}
		var volumeName string
		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// By("confirming that the lv was created in the thin volume group and pool")

		thinPVCCloneYAML := []byte(fmt.Sprintf(thinPvcCloneTemplateYAML, thinClonePVCName, "thinvol", pvcSize))
		stdout, stderr, err = kubectlWithInput(thinPVCCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodCloneYAML := []byte(fmt.Sprintf(thinPodCloneTemplateYAML, "thin-clone-pod", thinClonePVCName))
		stdout, stderr, err = kubectlWithInput(thinPodCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		By("confirming that the lv for cloned volume was created in the thin volume group and pool")

		Eventually(func() error {

			Eventually(func() error {
				volumeName, err = getVolumeNameofPVC(thinClonePVCName, nsCloneTest)
				return err
			}).Should(Succeed())

			var lv *thinlvinfo
			Eventually(func() error {
				lv, err = getThinLVInfo(volumeName)
				return err
			}).Should(Succeed())

			vgName := "node1-myvg4"
			if isDaemonsetLvmdEnvSet() {
				vgName = "node-myvg5"
			}
			Expect(vgName).Should(Equal(lv.vgName))

			poolName := "pool0"
			Expect(poolName).Should(Equal(lv.poolName))

			return err
		}).Should(Succeed())

	})

	It("validate if the cloned PVC is standalone", func() {
		By("deleting the source PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}

		var volumeName string

		// delete the source PVC and application
		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "delete", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "delete", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// validate if the cloned volume is present and is not deleted.

		Eventually(func() error {
			volumeName, err = getVolumeNameofPVC(thinClonePVCName, nsCloneTest)
			return err
		}).Should(Succeed())

		var lv *thinlvinfo
		Eventually(func() error {
			lv, err = getThinLVInfo(volumeName)
			return err
		}).Should(Succeed())

		vgName := "node1-myvg4"
		if isDaemonsetLvmdEnvSet() {
			vgName = "node-myvg5"
		}
		Expect(vgName).Should(Equal(lv.vgName))

		poolName := "pool0"
		Expect(poolName).Should(Equal(lv.poolName))

	})

}
