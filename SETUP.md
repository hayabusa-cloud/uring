# System Setup

Kernel upgrade, memlock configuration, and failure resolution for `uring`.

`uring` requires Linux 6.18+ and Go 1.26+. The examples below use Debian 13
(trixie).

## Kernel upgrade

Check the running kernel:

```bash
uname -r
```

If the version is below 6.18, upgrade before using `uring`.

### Debian 13 backports

Debian 13 ships kernel 6.12 in the stable suite. The `trixie-backports`
suite provides 6.18+.

1. Enable `trixie-backports` by creating
   `/etc/apt/sources.list.d/debian-backports.sources`:

   ```ini
   Types: deb deb-src
   URIs: http://deb.debian.org/debian
   Suites: trixie-backports
   Components: main
   Enabled: yes
   Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
   ```

   Skip this step if the suite is already present.

2. Refresh the index and verify the candidate version:

   ```bash
   sudo apt update
   apt policy linux-image-amd64 linux-headers-amd64
   ```

   Proceed once `apt policy` shows a `trixie-backports` candidate ≥ 6.18.

3. Install the kernel and headers:

   ```bash
   sudo apt install -t trixie-backports linux-image-amd64 linux-headers-amd64
   ```

4. Reboot and confirm:

   ```bash
   sudo reboot
   uname -r
   ```

The previous kernel remains selectable in the GRUB menu as a fallback.

## Memlock

`io_uring` pins memory for the SQ/CQ rings and for registered buffers.
The kernel charges this against `RLIMIT_MEMLOCK`. When the limit is too
low, ring creation or buffer registration returns `ENOMEM`.

### Check

```bash
ulimit -l
```

The output is in kilobytes. Many distributions default to 64 KB for
unprivileged accounts. That is too small for most `uring` configurations.

### Shell session

```bash
ulimit -l unlimited
```

This applies only to the current shell and its child processes.

### Persistent, limits.conf

Edit `/etc/security/limits.conf`:

```
*  -  memlock  unlimited
```

Or scope to a single user or group:

```
myuser   -  memlock  unlimited
@mygroup -  memlock  unlimited
```

Log out and back in for the change to take effect.

### Persistent, systemd

`/etc/security/limits.conf` does not apply to systemd-managed services.

Per-service override:

```bash
sudo systemctl edit myservice
```

```ini
[Service]
LimitMEMLOCK=infinity
```

For a system-wide default, edit `/etc/systemd/system.conf`:

```ini
DefaultLimitMEMLOCK=infinity
```

Reload and restart after either change:

```bash
sudo systemctl daemon-reload
sudo systemctl restart myservice
```

### Verify

Confirm inside the target process context:

```bash
grep "Max locked memory" /proc/self/limits
```

## Common failures

### ENOMEM, cannot lock memory

`io_uring_setup` or buffer registration returns `ENOMEM` when the kernel
cannot pin the required pages.

| Cause                    | Resolution                                                              |
|--------------------------|-------------------------------------------------------------------------|
| `RLIMIT_MEMLOCK` too low | Raise the memlock limit (see above)                                     |
| Queue depth too large    | Lower `Options.Entries` or reduce the buffer budget                     |
| Container memlock limit  | Raise the container memlock limit, for example `--ulimit memlock=-1:-1` |

### EPERM, io_uring disabled by sysctl

Some distributions ship `kernel.io_uring_disabled ≠ 0`. The kernel
returns `EPERM` from `io_uring_setup`.

```bash
sysctl kernel.io_uring_disabled
```

| Value | Effect                                           |
|-------|--------------------------------------------------|
| `0`   | All users may create rings (upstream default)    |
| `1`   | Restricted to members of `kernel.io_uring_group` |
| `2`   | Disabled for all users including root            |

Re-enable for the running session:

```bash
sudo sysctl -w kernel.io_uring_disabled=0
```

To persist across reboots, write to `/etc/sysctl.d/99-io_uring.conf`:

```
kernel.io_uring_disabled = 0
```

When the value is `1`, add the current user to the permitted group
instead of disabling the restriction:

```bash
GID=$(sysctl -n kernel.io_uring_group)
GROUP=$(getent group "$GID" | cut -d: -f1)
sudo usermod -aG "$GROUP" "$USER"
```

Log out and back in for group membership to take effect.

### ENOSYS, syscall absent

`ENOSYS` means the kernel does not expose `io_uring_setup`. Causes:

- kernel older than 5.1 (first release with `io_uring`);
- kernel built without `CONFIG_IO_URING`;
- seccomp profile blocking the syscall (common in containers).

Verify the config option:

```bash
grep CONFIG_IO_URING /boot/config-$(uname -r)
```

Expected: `CONFIG_IO_URING=y`.

Verify the syscall symbol:

```bash
grep io_uring_setup /proc/kallsyms
```

### Containers

Many container runtimes block `io_uring` syscalls in their default
seccomp profile. Ring creation inside the container returns `EPERM` or
`ENOSYS` even when the host kernel has full support.

Approaches, from most to least reliable:

1. **Run on the host or in a VM.** The seccomp filter layer in container
   runtimes varies across versions and may silently re-block syscalls
   after an upgrade.

2. **Disable seccomp** (development only):

   ```bash
   docker run --security-opt seccomp=unconfined ...
   ```

   Some hardened runtimes enforce an additional block that overrides
   this flag.

3. **Custom seccomp profile.** Copy the runtime's default profile, add
   `io_uring_setup`, `io_uring_enter`, and `io_uring_register` to the
   allowed list, then pass it at container start:

   ```bash
   docker run --security-opt seccomp=custom.json ...
   ```

4. **Check the host sysctl.** `kernel.io_uring_disabled` on the host
   applies inside the container regardless of the seccomp configuration.

## License

MIT, see [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)
