Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not ("WindowsAgent.AssistSetup.ProcessIdentity" -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Security.Principal;
using System.Text;

namespace WindowsAgent.AssistSetup {
    public sealed class ProcessIdentity {
        public uint ProcessId;
        public uint SessionId;
        public bool Elevated;
        public string UserSid;
        public string ExecutablePath;

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern IntPtr OpenProcess(uint access, bool inherit, uint processId);
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool CloseHandle(IntPtr handle);
        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern bool QueryFullProcessImageName(IntPtr process, uint flags, StringBuilder path, ref uint length);
        [DllImport("advapi32.dll", SetLastError = true)]
        private static extern bool OpenProcessToken(IntPtr process, uint access, out IntPtr token);
        [DllImport("advapi32.dll", SetLastError = true)]
        private static extern bool GetTokenInformation(IntPtr token, int informationClass, out uint value, uint length, out uint returnedLength);

        private static uint ReadTokenValue(IntPtr token, int informationClass) {
            uint value, length;
            if (!GetTokenInformation(token, informationClass, out value, sizeof(uint), out length))
                throw new Win32Exception(Marshal.GetLastWin32Error(), "read Capture Agent token information");
            if (length != sizeof(uint))
                throw new InvalidOperationException("Capture Agent token information has an unexpected size");
            return value;
        }

        public static ProcessIdentity Read(uint processId) {
            IntPtr process = OpenProcess(0x1000, false, processId);
            if (process == IntPtr.Zero)
                throw new Win32Exception(Marshal.GetLastWin32Error(), "open Capture Agent process for verification");
            IntPtr token = IntPtr.Zero;
            try {
                if (!OpenProcessToken(process, 0x0008, out token))
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "open Capture Agent token for verification");
                uint length = 32768;
                StringBuilder path = new StringBuilder((int)length);
                if (!QueryFullProcessImageName(process, 0, path, ref length))
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "read Capture Agent executable identity");
                using (WindowsIdentity identity = new WindowsIdentity(token)) {
                    return new ProcessIdentity {
                        ProcessId = processId,
                        SessionId = ReadTokenValue(token, 12),
                        Elevated = ReadTokenValue(token, 20) != 0,
                        UserSid = identity.User.Value,
                        ExecutablePath = path.ToString()
                    };
                }
            } finally {
                if (token != IntPtr.Zero) CloseHandle(token);
                CloseHandle(process);
            }
        }
    }
}
'@
}

function Assert-AssistProcessIdentity {
    param(
        [Parameter(Mandatory = $true)]$Identity,
        [Parameter(Mandatory = $true)][string]$ExpectedExecutablePath,
        [Parameter(Mandatory = $true)][uint32]$ExpectedSessionId,
        [Parameter(Mandatory = $true)][string]$ExpectedUserSid
    )
    if (-not [string]::Equals([IO.Path]::GetFullPath($Identity.ExecutablePath), [IO.Path]::GetFullPath($ExpectedExecutablePath), [StringComparison]::OrdinalIgnoreCase)) {
        throw "Capture Agent listener executable does not match the installed artifact"
    }
    if ($ExpectedSessionId -eq 0 -or $Identity.SessionId -ne $ExpectedSessionId) {
        throw "Capture Agent is not running in the installer's interactive session"
    }
    if ($Identity.UserSid -cne $ExpectedUserSid) {
        throw "Capture Agent is not running as the installing user"
    }
    if (-not $Identity.Elevated) {
        throw "Capture Agent process token is not elevated"
    }
}

function Assert-AssistRuntimeReady {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath)
    $listenerProcessIds = @(Get-NetTCPConnection -State Listen -LocalPort 8787 -ErrorAction Stop |
        Select-Object -ExpandProperty OwningProcess -Unique)
    if ($listenerProcessIds.Count -ne 1) {
        throw "Capture Agent port 8787 must have exactly one owning process"
    }
    $identity = [WindowsAgent.AssistSetup.ProcessIdentity]::Read([uint32]$listenerProcessIds[0])
    $installerSessionId = [Diagnostics.Process]::GetCurrentProcess().SessionId
    $installerIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
    try {
        Assert-AssistProcessIdentity -Identity $identity -ExpectedExecutablePath $ExecutablePath -ExpectedSessionId $installerSessionId -ExpectedUserSid $installerIdentity.User.Value
    } finally {
        $installerIdentity.Dispose()
    }
    [pscustomobject]@{pid=$identity.ProcessId; sessionId=$identity.SessionId; elevated=$identity.Elevated}
}
