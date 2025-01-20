package integration

import (
	"testing"
	"time"

	"github.com/hugelgupf/vmtest/qemu"
	"github.com/hugelgupf/vmtest/scriptvm"
	"github.com/u-root/mkuimage/uimage"
)

func TestBrctlAdd(t *testing.T) {
	//qemu.SkipIfNotArch(t, qemu.ArchAMD64)

	script := `
	brctl addbr dummy-br0
	brctl show
	brctl delbr dummy-br0
	brctl show
	`

	vm := scriptvm.Start(t, "vm", script,
		scriptvm.WithUimage(
			uimage.WithBusyboxCommands("github.com/u-root/u-root/cmds/core/brctl"),
		),
		scriptvm.WithQEMUFn(
			qemu.WithVMTimeout(30*time.Second),
			qemu.VirtioRandom(),
		),
	)

	if _, err := vm.Console.ExpectString("dummy-br0"); err != nil {
		t.Error(`expected "dummy-br0", got error: `, err)
	}
	if _, err := vm.Console.ExpectString("dummy-br0"); err == nil {
		t.Error(`found "dummy-br0", but should be deleted`)
	}
	if err := vm.Wait(); err != nil {
		t.Errorf("Wait: %v", err)
	}
}
