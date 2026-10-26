package buildinfo

func init() {
	registerSSHTransport(SSHTransport{
		ID:      "scrapligo-v1",
		Name:    "scrapligo",
		Version: "1.4.3",
		Linkage: "compiled-in",
	})
}
