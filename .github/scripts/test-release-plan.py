import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("release-plan.py").resolve()


class ReleasePlanning(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "VERSION").write_text("2.0.0\n")
        self.git("init", "-q")
        self.git("config", "user.name", "Release test")
        self.git("config", "user.email", "release@example.invalid")
        self.git("add", "VERSION")
        self.git("commit", "-qm", "Version")
        self.sha = self.git("rev-parse", "HEAD").stdout.strip()
        self.git("update-ref", "refs/remotes/origin/main", self.sha)

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.root, capture_output=True, text=True, check=True)

    def plan(self, event="pull_request", ref="refs/pull/1/merge", sha=None):
        output = self.root / "outputs"
        output.unlink(missing_ok=True)
        environment = {**os.environ, "GITHUB_EVENT_NAME": event, "GITHUB_REF": ref,
                       "GITHUB_SHA": sha or self.sha, "GITHUB_OUTPUT": str(output)}
        result = subprocess.run([os.sys.executable, str(SCRIPT)], cwd=self.root,
                                env=environment, capture_output=True, text=True)
        values = dict(line.split("=", 1) for line in output.read_text().splitlines()) if output.exists() else {}
        return result, values

    def test_feature_and_dev_never_publish(self):
        for event, ref in [("pull_request", "refs/pull/1/merge"), ("push", "refs/heads/dev"),
                           ("workflow_dispatch", "refs/heads/codex/feature")]:
            result, values = self.plan(event, ref)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(values["release"], "false")

    def test_new_main_version_is_released_automatically(self):
        result, values = self.plan("push", "refs/heads/main")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(values, {"version": "2.0.0", "minor": "2.0", "tag": "v2.0.0", "release": "true"})

    def test_existing_tag_is_never_republished_by_main_push(self):
        self.git("tag", "-a", "v2.0.0", "-m", "Released")
        result, values = self.plan("push", "refs/heads/main")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(values["release"], "false")

    def test_unreviewed_commit_cannot_be_released(self):
        (self.root / "other").write_text("unreviewed")
        self.git("add", "other")
        self.git("commit", "-qm", "Other branch")
        sha = self.git("rev-parse", "HEAD").stdout.strip()
        result, _ = self.plan("push", "refs/heads/main", sha)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("reviewed main", result.stderr)

    def test_package_metadata_must_agree(self):
        (self.root / "package.json").write_text(json.dumps({"version": "1.0.0"}))
        result, _ = self.plan()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must match", result.stderr)

    def test_stable_versions_are_canonical(self):
        for value in ["02.0.0", "2.0", "v2.0.0", "2.0.0\nextra"]:
            (self.root / "VERSION").write_text(value)
            result, _ = self.plan()
            self.assertNotEqual(result.returncode, 0, value)

    def test_tag_must_match_version(self):
        result, _ = self.plan("push", "refs/tags/v2.1.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must match VERSION", result.stderr)


if __name__ == "__main__":
    unittest.main()
