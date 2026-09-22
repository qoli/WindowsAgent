# Initialize a Harness from AssistGUI

Use this flow when the user pastes the prompt copied by AssistGUI and it names
one WindowsAgent host.

1. Locate the installed `use-windows-pc` Skill. If it is unavailable, download
   the official stable bundle from:

   ```text
   https://github.com/qoli/WindowsAgent/releases/latest/download/windowsagent-user-skills.zip
   ```

   Extract its three Skill directories into the Harness's normal user Skill
   location. Install only the stable Skills included in that bundle.
2. Validate that the supplied value is one hostname or IP address without a
   scheme, credential, port, path, query, or fragment.
3. Create `${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}` with only:

   ```text
   WINDOWS_AGENT_HOST=<supplied host>
   ```

   Create the parent directory if needed, write the file atomically, and set
   mode `0600`. Do not copy private connection data into chat or tracked files.
4. Source `scripts/resolve-pc.sh`, require `/healthz` to report `status: ok`, and
   request a fresh capture with `scripts/capture.sh`.
5. Verify the normal read-only control path with the bundled portable client by
   running `C:\Windows\System32\whoami.exe`. Preserve its invocation identity
   and terminal result. This proves structured execution, not any later desktop
   or application goal.

Report installation/configuration, transport, health, fresh capture identity,
and structured execution separately. Do not stop after describing these steps.
