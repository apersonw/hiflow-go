package iamserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 契约自检：HIFLOW_IAMCHECK 为 HiFlow tools/iamcheck 的可执行文件，HIFLOW_IAM_DATA 为示例组织数据（make test-iam-skeleton）
func TestContract(t *testing.T) {
	bin, data := os.Getenv("HIFLOW_IAMCHECK"), os.Getenv("HIFLOW_IAM_DATA")
	if bin == "" || data == "" {
		t.Skip("未设置 HIFLOW_IAMCHECK / HIFLOW_IAM_DATA")
	}
	d, err := LoadStaticFile(data)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/hiflow/v1/", http.StripPrefix("/hiflow/v1", New(NewStatic(d), Options{DirectoryID: testID, Secret: testSecret, RecursiveMembers: true, RoleDepartmentScope: true})))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out, err := exec.Command(bin, "-base", srv.URL+"/hiflow/v1", "-id", testID, "-secret", testSecret).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "失败 0") {
		t.Fatalf("iamcheck: %v\n%s", err, out)
	}
}
