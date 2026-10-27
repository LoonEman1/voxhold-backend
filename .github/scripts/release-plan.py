"""Plan immutable, versioned releases from reviewed main commits."""
import json
import os
from pathlib import Path
import re
import subprocess


def command(*args):
    return subprocess.run(args, text=True, capture_output=True, check=False)


def main():
    version = Path("VERSION").read_text().strip()
    if not re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", version):
        raise SystemExit("VERSION must contain a stable major.minor.patch version")
    package = Path("package.json")
    if package.exists() and json.loads(package.read_text())["version"] != version:
        raise SystemExit("package.json and VERSION must match")
    tag = "v" + version
    event, ref = os.environ["GITHUB_EVENT_NAME"], os.environ["GITHUB_REF"]
    sha = os.environ["GITHUB_SHA"]
    release = False
    eligible = event in ("push", "workflow_dispatch") and (ref == "refs/heads/main" or ref.startswith("refs/tags/"))
    if eligible:
        if ref.startswith("refs/tags/") and ref != "refs/tags/" + tag:
            raise SystemExit("Release tag must match VERSION")
        if command("git", "merge-base", "--is-ancestor", sha, "origin/main").returncode:
            raise SystemExit("Release commit must already belong to reviewed main")
        existing = command("git", "rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}")
        if ref.startswith("refs/tags/"):
            if existing.returncode or existing.stdout.strip() != sha:
                raise SystemExit("Release tag must identify this exact commit")
            result = command("gh", "api", f"repos/{os.environ['GITHUB_REPOSITORY']}/releases/tags/{tag}")
            if result.returncode:
                # Distinguish a missing release from authentication/network errors.
                if "HTTP 404" not in result.stderr:
                    raise SystemExit("Cannot inspect existing release: " + result.stderr)
                release = True
            else:
                release = bool(json.loads(result.stdout).get("draft"))
        else:
            # Existing versions are never rebuilt or moved by unrelated main pushes.
            release = existing.returncode != 0
    outputs = {"version": version, "minor": ".".join(version.split(".")[:2]), "tag": tag, "release": str(release).lower()}
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as stream:
        stream.write("".join(f"{key}={value}\n" for key, value in outputs.items()))
    print(json.dumps(outputs))


if __name__ == "__main__":
    main()
