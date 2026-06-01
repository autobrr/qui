---
sidebar_position: 99
title: License Management
description: Manage premium theme license activations, deactivate old servers, and recover lost keys.
---

# License Management

Premium themes require a license key. Each key has a limited number of activation slots — one per server where qui is running.

## Activating a License

1. Open **Settings → Themes** in your qui instance.
2. Click **Add License** and enter your license key.
3. Premium themes are available immediately after activation.

## Moving a License to a New Server

If you are replacing a server or reinstalling, you need to free the activation slot used by the old instance before the key will work on the new one.

### If the old server is still accessible

1. Open qui on the **old** server.
2. Go to **Settings → Themes** and click **Remove** next to the license.
3. This deactivates the license on that machine and frees the slot.
4. Activate the same key on the new server.

### If the old server is gone

When the old server no longer exists (hardware failure, destroyed VPS, etc.) you cannot remove the license from within qui. Use the license portal instead:

1. Go to [licenses.getqui.com](https://licenses.getqui.com/).
2. Register or log in with **the same email address you used to purchase the license**.
3. Find your license and deactivate the old activation.
4. Activate the key on your new server from **Settings → Themes**.

## Recovering a Lost License Key

Log in to [licenses.getqui.com](https://licenses.getqui.com/) with the email address you used at checkout. Your license keys are listed there.

## Troubleshooting

### "License activation limit has been reached"

All activation slots for your key are in use. Deactivate an old activation either from the other qui instance (**Settings → Themes → Remove**) or through [licenses.getqui.com](https://licenses.getqui.com/) if that server is no longer available.

### "This license was activated on a different machine"

This means the qui database was copied from another server. The stored activation does not match this machine's identity. Remove the license in **Settings → Themes → Remove**, then re-activate it with your key.

### "Unable to reach the license service"

The license server is temporarily unreachable. Wait a moment and try again. If the issue persists, check your server's outbound network connectivity.
