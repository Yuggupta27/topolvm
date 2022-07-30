package e2e

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	//	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

//go:embed testdata/thin_provisioning/pod-template.yaml
var thinPodTemplateYAML string

//go:embed testdata/thin_provisioning/pvc-template.yaml
var thinPVCTemplateYAML string

func testThinProvisioning() {
	testNamespacePrefix := "thinptest-"
	var ns string
	// var cc CleanupContext
	BeforeEach(func() {
		// cc = commonBeforeEach()
		ns = testNamespacePrefix + randomString(10)
		createNamespace(ns)
	})

	AfterEach(func() {
		// When a test fails, I want to investigate the cause. So please don't remove the namespace!
		if !CurrentGinkgoTestDescription().Failed {
			kubectl("delete", "namespaces/"+ns)
		}
		// commonAfterEach(cc)
	})

	It("should thin provision a PV", func() {
		By("deploying Pod with PVC")

		nodeName := "topolvm-e2e-worker"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}

		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, "thinvol", "1"))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", "thinvol", nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		By("confirming that the lv was created in the thin volume group and pool")
		var volumeName string

		Eventually(func() error {
			volumeName, err = getVolumeNameofPVC("thinvol", ns)
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

	It("should overprovision thin PVCs", func() {
		By("deploying multiple PVCS with total size < thinpoolsize * overprovisioning")
		// The actual thinpool size is 4 GB . With an overprovisioning limit of 5, it should allow
		// PVCs totalling upto 20 GB
		nodeName := "topolvm-e2e-worker2"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}
		for i := 0; i < 5; i++ {
			num := strconv.Itoa(i)
			thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, "thinvol"+num, "3"))
			stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", ns, "-f", "-")
			Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

			thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod"+num, "thinvol"+num, nodeName))
			stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", ns, "-f", "-")
			Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		}

		By("confirming that the volumes have been created in the thinpool")

		for i := 0; i < 5; i++ {
			var volumeName string
			var err error

			num := strconv.Itoa(i)
			Eventually(func() error {
				volumeName, err = getVolumeNameofPVC("thinvol"+num, ns)
				return err
			}).Should(Succeed())

			var lv *thinlvinfo
			Eventually(func() error {
				lv, err = getThinLVInfo(volumeName)
				return err
			}).Should(Succeed())

			vgName := "node2-myvg4"
			if isDaemonsetLvmdEnvSet() {
				vgName = "node-myvg5"
			}
			Expect(vgName).Should(Equal(lv.vgName))

			poolName := "pool0"
			Expect(poolName).Should(Equal(lv.poolName))
		}
	})

	It("should check overprovision limits", func() {
		By("Deploying a PVC to use up the available thinpoolsize * overprovisioning")

		nodeName := "topolvm-e2e-worker3"
		if isDaemonsetLvmdEnvSet() {
			nodeName = getDaemonsetLvmdNodeName()
		}

		thinPvcYAML := []byte(fmt.Sprintf(thinPVCTemplateYAML, "thinvol", "18"))
		stdout, stderr, err := kubectlWithInput(thinPvcYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML := []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod", "thinvol", nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		var volumeName string
		Eventually(func() error {
			volumeName, err = getVolumeNameofPVC("thinvol", ns)
			return err
		}).Should(Succeed())

		var lv *thinlvinfo
		Eventually(func() error {
			lv, err = getThinLVInfo(volumeName)
			return err
		}).Should(Succeed())

		vgName := "node3-myvg4"
		if isDaemonsetLvmdEnvSet() {
			vgName = "node-myvg5"
		}
		Expect(vgName).Should(Equal(lv.vgName))

		poolName := "pool0"
		Expect(poolName).Should(Equal(lv.poolName))

		By("Failing to deploying a PVC when total size > thinpoolsize * overprovisioning")
		thinPvcYAML = []byte(fmt.Sprintf(thinPVCTemplateYAML, "thinvol2", "5"))
		stdout, stderr, err = kubectlWithInput(thinPvcYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		thinPodYAML = []byte(fmt.Sprintf(thinPodTemplateYAML, "thinpod2", "thinvol2", nodeName))
		stdout, stderr, err = kubectlWithInput(thinPodYAML, "apply", "-n", ns, "-f", "-")
		Expect(err).ShouldNot(HaveOccurred(), "stdout=%s, stderr=%s", stdout, stderr)

		Eventually(func() error {
			stdout, stderr, err := kubectl("get", "-n", ns, "pvc", "thinvol2", "-o", "json")
			if err != nil {
				return fmt.Errorf("failed to get PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
			}

			var pvc corev1.PersistentVolumeClaim
			err = json.Unmarshal(stdout, &pvc)
			if err != nil {
				return fmt.Errorf("failed to unmarshal PVC. stdout: %s, err: %v", stdout, err)
			}
			if pvc.Status.Phase == corev1.ClaimBound {
				return fmt.Errorf("PVC should not be bound")
			}
			/*
				volName, err := getVolumeNameofPVC("thinvol2", ns)
				if err != nil {
					return fmt.Errorf("failed to get volume name. err: %v", err)
				}
				stdout, stderr, err := kubectl("get", "logicalvolume", volName, "-o", "json")
				if err != nil {
					return fmt.Errorf("failed to get logical volume. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
				}
				expectedMessage := "no enough space left on VG"
				var lv topolvmv1.LogicalVolume
				err = json.Unmarshal(stdout, &lv)
				if err != nil {
					return err
				}
				items := strings.Split(strings.TrimSpace(lv.Status.Message), ":")
				statusMsg := strings.TrimSpace(items[0])
				if (lv.Status.Code == 8) || (statusMsg == expectedMessage) {
					return nil
				}
				return fmt.Errorf("Failed to get expected logicalVolume status")
			*/
			return nil
		}).Should(Succeed())

	})
}

type thinlvinfo struct {
	lvName   string
	poolName string
	vgName   string
}

func getThinLVInfo(volName string) (*thinlvinfo, error) {
	stdout, stderr, err := kubectl("get", "logicalvolume", "-n", "topolvm-system", volName, "-o=template", "--template={{.metadata.uid}}")
	if err != nil {
		return nil, fmt.Errorf("err=%v, stdout=%s, stderr=%s", err, stdout, stderr)
	}

	lvName := strings.TrimSpace(string(stdout))
	stdout, err = exec.Command("sudo", "lvs", "--noheadings", "-o", "lv_name,pool_lv,vg_name", "--select", "lv_name="+lvName).Output()
	if err != nil {
		return nil, fmt.Errorf("err=%v, stdout=%s", err, stdout)
	}
	output := strings.TrimSpace(string(stdout))
	if output == "" {
		return nil, fmt.Errorf("lv_name ( %s ) not found", lvName)
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 1 {
		return nil, errors.New("found multiple lvs")
	}
	items := strings.Fields(strings.TrimSpace(lines[0]))
	if len(items) < 3 {
		return nil, fmt.Errorf("invalid format: %s", lines[0])
	}
	return &thinlvinfo{lvName: items[0], poolName: items[1], vgName: items[2]}, nil
}

func getVolumeNameofPVC(pvcName, ns string) (volName string, err error) {
	stdout, stderr, err := kubectl("get", "-n", ns, "pvc", pvcName, "-o", "json")
	if err != nil {
		return "", fmt.Errorf("failed to get PVC. stdout: %s, stderr: %s, err: %v", stdout, stderr, err)
	}

	var pvc corev1.PersistentVolumeClaim
	err = json.Unmarshal(stdout, &pvc)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal PVC. stdout: %s, err: %v", stdout, err)
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return "", errors.New("pvc status is not bound")
	}
	if pvc.Spec.VolumeName == "" {
		return "", errors.New("pvc.Spec.VolumeName should not be empty")
	}

	volumeName := pvc.Spec.VolumeName
	return volumeName, nil
}
