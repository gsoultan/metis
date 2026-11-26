package selfupgrade

import "testing"

func TestPackageName(t *testing.T) {
	tests := []struct {
		f      Format
		pkg    string
		goarch string
		want   string
	}{
		{FormatDeb, "metis", "amd64", "metis_0.7.3_amd64.deb"},
		{FormatDeb, "metis", "arm64", "metis_0.7.3_arm64.deb"},
		{FormatRPM, "metis", "amd64", "metis-0.7.3-1.x86_64.rpm"},
		{FormatAPK, "metis", "arm64", "metis_0.7.3_aarch64.apk"},
		{FormatArch, "metis", "amd64", "metis-0.7.3-1-x86_64.pkg.tar.zst"},
		// metis-server's packages, named by the same GoReleaser convention.
		{FormatDeb, "metis-server", "amd64", "metis-server_0.7.3_amd64.deb"},
		{FormatRPM, "metis-server", "arm64", "metis-server-0.7.3-1.aarch64.rpm"},
		{FormatAPK, "metis-server", "amd64", "metis-server_0.7.3_x86_64.apk"},
		{FormatArch, "metis-server", "arm64", "metis-server-0.7.3-1-aarch64.pkg.tar.zst"},
	}
	for _, tt := range tests {
		got, err := PackageName(tt.f, tt.pkg, "v0.7.3", tt.goarch)
		if err != nil || got != tt.want {
			t.Errorf("PackageName(%s, %s, %s) = %q, %v; want %q", tt.f, tt.pkg, tt.goarch, got, err, tt.want)
		}
	}
	if _, err := PackageName(FormatDeb, "metis", "v0.7.3", "riscv64"); err == nil {
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

func TestQueryCommandAsksForTheInstalledPackage(t *testing.T) {
	for _, f := range []Format{FormatDeb, FormatRPM, FormatAPK, FormatArch} {
		_, args := QueryCommand(f, "metis-server")
		if args[len(args)-1] != "metis-server" {
			t.Errorf("QueryCommand(%s) = %v, want it to ask for metis-server", f, args)
		}
	}
}
