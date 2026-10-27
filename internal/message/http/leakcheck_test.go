package messagehttp

import (
	"os"
	"testing"
	"voxhold-backend/internal/leaktest"
)

func TestMain(m *testing.M) { os.Exit(leaktest.Run(m)) }
