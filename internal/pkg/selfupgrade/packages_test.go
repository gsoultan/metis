package selfupgrade

import "testing"

func TestPackageName(t *testing.T) {
	tests := []struct {
		f      Format
		goarch string
		want   string
	}{
		{FormatDeb, "amd64", "metis_0.7.3_amd64.deb"},
		{FormatDeb, "arm64", "metis_0.7.3_arm64.deb"},
		{FormatRPM, "amd64", "metis-0.7.3-1.x86_64.rpm"},
		{FormatAPK, "arm64", "metis_0.7.3_aarch64.apk"},
		{FormatArch, "amd64", "metis-0.7.3-1-x86_64.pkg.tar.zst"},
	}
	for _, tt := range tests {
		got, err := PackageName(tt.f, "v0.7.3", tt.goarch)
		if err != nil || got != tt.want {
			t.Errorf("PackageName(%s, %s) = %q, %v; want %q", tt.f, tt.goarch, got, err, tt.want)
		}
	}
	if _, err := PackageName(FormatDeb, "v0.7.3", "riscv64"); err == nil {
		t.Error("expected an architecture nothing is published for to be refused")
	}
}

func TestDetectFormat(t *testing.T) {
	if f, err := DetectFormat(func(f Format) bool { return f == FormatRPM }); err != nil || f != FormatRPM {
		t.Fatalf("got %q, %v", f, err)
	}
	if _, err := DetectFormat(func(Format) bool { return false }); err == nil {
		t.Fatal("expected a host with no package to be refused")
	}
}

func TestInstallCommandAllowsTheRollbackDowngrade(t *testing.T) {
	name, args, err := InstallCommand(FormatRPM, "/x.rpm")
	if err != nil || name != "rpm" || args[1] != "--oldpackage" {
		t.Fatalf("got %s %v %v", name, args, err)
	}
}
