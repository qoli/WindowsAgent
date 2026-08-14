#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "$0")/../.." && pwd)"
tool_dir="$repo_dir/tools/supercruise-sphere-direction"
output_dir="$repo_dir/Rules/EliteDangerous64.exe/Actions/supercruise-sphere-direction/native/windows-amd64"

rustup_cargo="$(rustup which cargo)"
rustup_rustc="$(dirname "$rustup_cargo")/rustc"
RUSTC="$rustup_rustc" "$rustup_cargo" build \
  --manifest-path "$tool_dir/Cargo.toml" \
  --release \
  --target x86_64-pc-windows-gnu
mkdir -p "$output_dir"
cp "$tool_dir/target/x86_64-pc-windows-gnu/release/elite_supercruise_sphere_cv.dll" \
  "$output_dir/elite-supercruise-sphere-cv.dll"
