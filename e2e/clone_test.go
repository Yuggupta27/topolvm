package e2e

import (
	_ "embed"
	"fmt"
	"strings"

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

		By("confirming if the source PVC and application have been created")
		Eventually(func() error {
			stdout, stderr, err = kubectl("get", "pvc", volName, "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			stdout, stderr, err = kubectl("get", "pods", "thinpod", "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create Pod. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			return nil
		}).Should(Succeed())
		// By("confirming that the lv was created in the thin volume group and pool")

		By("writing file under /test1")
		writePath := "/test1/bootstrap.log"
		Eventually(func() error {
			stdout, stderr, err = kubectl("exec", "-n", nsCloneTest, "thinpod", "--", "cp", "/var/log/bootstrap.log", writePath)
			return err
		}).Should(Succeed())

		stdout, stderr, err = kubectl("exec", "-n", nsCloneTest, "thinpod", "--", "sync")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)
		stdout, stderr, err = kubectl("exec", "-n", nsCloneTest, "thinpod", "--", "cat", writePath)
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)
		Expect(strings.TrimSpace(string(stdout))).ShouldNot(BeEmpty())

		thinPVCCloneYAML := []byte(fmt.Sprintf(thinPvcCloneTemplateYAML, thinClonePVCName, "thinvol", pvcSize))
		stdout, stderr, err = kubectlWithInput(thinPVCCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodCloneYAML := []byte(fmt.Sprintf(thinPodCloneTemplateYAML, "thin-clone-pod", thinClonePVCName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		By("confirming that the lv for cloned volume was created in the thin volume group and pool")

		// Eventually(func() error {

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

		By("confirming that the file exists in the cloned volume")
		Eventually(func() error {
			stdout, stderr, err = kubectl("get", "pvc", thinClonePVCName, "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			stdout, stderr, err = kubectl("get", "pods", "thin-clone-pod", "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create Pod. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			stdout, stderr, err = kubectl("exec", "-n", nsCloneTest, "thin-clone-pod", "--", "cat", writePath)
			if err != nil {
				return fmt.Errorf("failed to cat. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}
			if len(strings.TrimSpace(string(stdout))) == 0 {
				return fmt.Errorf(writePath + " is empty")
			}
			return nil
		}).Should(Succeed())
		// 	return err
		// }).Should(Succeed())

	})

	It("validate if the cloned PVC is standalone", func() {
		// By("deleting the source PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}
		By("creating a PVC")
		var volumeName string
		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)
		Eventually(func() error {
			stdout, stderr, err = kubectl("get", "pvc", volName, "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			stdout, stderr, err = kubectl("get", "pods", "thinpod", "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create Pod. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			return nil
		}).Should(Succeed())
		By("creating clone of PVC")
		thinPVCCloneYAML := []byte(fmt.Sprintf(thinPvcCloneTemplateYAML, thinClonePVCName, "thinvol", pvcSize))
		stdout, stderr, err = kubectlWithInput(thinPVCCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodCloneYAML := []byte(fmt.Sprintf(thinPodCloneTemplateYAML, "thin-clone-pod", thinClonePVCName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodCloneYAML, "apply", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)
		Eventually(func() error {

			stdout, stderr, err = kubectl("get", "pvc", thinClonePVCName, "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			stdout, stderr, err = kubectl("get", "pods", "thin-clone-pod", "-n", nsCloneTest)
			if err != nil {
				return fmt.Errorf("failed to create Pod. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}
			return nil
		}).Should(Succeed())
		// delete the source PVC and application
		// thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		// By("deleting the source PVC and application")
		// stdout, stderr, err = kubectlWithInput(thinPvcYAML, "delete", "-n", nsCloneTest, "-f", "-")
		// Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// //thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		// stdout, stderr, err = kubectlWithInput(thinPodYAML, "delete", "-n", nsCloneTest, "-f", "-")
		// Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// validate if the cloned volume is present.
		By("validate if the cloned volume is present")
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

		// delete the source PVC and application
		By("deleting source volume and snapshot")
		thinPodYAML = []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "delete", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPvcYAML = []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err = kubectlWithInput(thinPvcYAML, "delete", "-n", nsCloneTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		By("validate if the cloned volume is present and is not deleted")
		Eventually(func() error {
			volumeName, err = getVolumeNameofPVC(thinClonePVCName, nsCloneTest)
			return err
		}).Should(Succeed())

		Eventually(func() error {
			lv, err = getThinLVInfo(volumeName)
			return err
		}).Should(Succeed())
	})

}
