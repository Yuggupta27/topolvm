package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"

	//	"strconv"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (

	//go:embed testdata/snapshot_restore/snapshotclass.yaml
	thinSnapshotClassYAML string

	//go:embed testdata/snapshot_restore/snapshot-template.yaml
	thinSnapshotTemplateYAML string

	//go:embed testdata/snapshot_restore/restore-pvc-template.yaml
	thinRestorePVCTemplateYAML string

	//go:embed testdata/snapshot_restore/restore-pod-template.yaml
	thinRestorePodTemplateYAML string
)

const (
	nsSnapTest     = "snap-test"
	volName        = "thinvol"
	snapName       = "thinsnap"
	restorePVCName = "thinrestore"
	// size of PVC in GBs
	pvcSize = "1"
)

func testSnapRestore() {
	// testNamespace
	var cc CleanupContext
	BeforeEach(func() {
		cc = commonBeforeEach()
		createNamespace(nsSnapTest)
	})
	AfterEach(func() {
		if !CurrentGinkgoTestDescription().Failed {
			kubectl("delete", "namespaces/"+nsSnapTest)
		}
		commonAfterEach(cc)
	})

	It("should create a thin-snapshot", func() {
		By("deploying Pod with PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}
		var volumeName string
		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// By("confirming that the lv was created in the thin volume group and pool")

		thinSnapshotClassYAML := []byte(fmt.Sprintf(thinSnapshotClassYAML, "topolvm-provisioner-thin"))
		stdout, stderr, err = kubectlWithInput(thinSnapshotClassYAML, "apply", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinSnapshotYAML := []byte(fmt.Sprintf(thinSnapshotTemplateYAML, snapName, "thinvol"))
		stdout, stderr, err = kubectlWithInput(thinSnapshotYAML, "apply", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		By("confirming that the lv was created in the thin volume group and pool")
		var bound bool

		Eventually(func() error {
			bound, err = getSnapshotStatus(snapName, nsSnapTest)
			if !bound {
				return fmt.Errorf("the snapshot %s failed to reach status BOUND", snapName)
			}

			thinPVCRestoreYAML := []byte(fmt.Sprintf(thinRestorePVCTemplateYAML, volName, pvcSize))
			stdout, stderr, err := kubectlWithInput(thinPVCRestoreYAML, "apply", "-n", nsSnapTest, "-f", "-")
			Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

			thinPVCRestorePodYAML := []byte(fmt.Sprintf(thinRestorePodTemplateYAML, "thinpod", volName, nodeName))
			stdout, stderr, err = kubectlWithInput(thinPVCRestorePodYAML, "apply", "-n", nsSnapTest, "-f", "-")
			Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

			Eventually(func() error {
				volumeName, err = getVolumeNameofPVC(restorePVCName, nsSnapTest)
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

	It("validate if the restored PVCs are standalone", func() {
		By("deleting the source PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}

		var volumeName string

		// delete the source PVC as well as the snapshot
		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, volName, pvcSize))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "delete", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", volName, nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "delete", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinSnapshotYAML := []byte(fmt.Sprintf(thinSnapshotTemplateYAML, snapName, volName))
		stdout, stderr, err = kubectlWithInput(thinSnapshotYAML, "delete", "-n", nsSnapTest, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		// validate if the restored volume is present and is not deleted.

		Eventually(func() error {
			volumeName, err = getVolumeNameofPVC(restorePVCName, nsSnapTest)
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

// getSnapshotStatus validates if the VolumeSnapshot is ready to use.
func getSnapshotStatus(snapName, ns string) (status bool, err error) {
	stdout, stderr, err := kubectl("get", "-n", ns, "vs", snapName, "-o", "json")
	if err != nil {
		return false, fmt.Errorf("failed to get Snapshot. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
	}

	var snap *snapapi.VolumeSnapshot
	err = json.Unmarshal(stdout, &snap)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal VolumeSnapshot. stdout: %s, err: %v", stdout, err)
	}

	if *snap.Status.ReadyToUse {
		return true, nil
	}
	// time.Sleep(time.Second)
	return false, fmt.Errorf("VolumeSnapshot %s not ready to use", snapName)
}
