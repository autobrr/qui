---
sidebar_position: 99
title: License Management
description: Manage premium theme license activations, deactivate old servers, and recover lost keys.
---

# License Management

Premium themes require a license key. Each key has a limited number of activation slots, one for each server that runs qui. The same license also unlocks [custom themes](./features/custom-themes.md), which you sideload as CSS. The [supporter subscription](./support.md) is separate: a recurring donation that issues no key.

## Activate a license

1. Open **Settings → Premium Themes** in your qui instance.
2. Click **Add License** and enter your license key.
3. Premium themes unlock after activation.

## Move a license to a new server

If you replace a server or reinstall, first free the activation slot that the old instance uses. The key will not work on the new server until you do.

### If the old server is still accessible

1. Open qui on the **old** server.
2. Go to **Settings → Premium Themes** and click **Remove** next to the license.
3. qui deactivates the license on that machine and frees the slot.
4. Activate the same key on the new server.

### If the old server is gone

If the old server no longer exists, for example after a hardware failure or a destroyed VPS, you cannot remove the license from within qui. Use the license portal instead:

1. Go to [licenses.getqui.com](https://licenses.getqui.com/).
2. Register or log in with **the same email address you used to purchase the license**.
3. Find your license and deactivate the old activation.
4. Activate the key on your new server from **Settings → Premium Themes**.

## Recover a lost license key

Log in to [licenses.getqui.com](https://licenses.getqui.com/) with the email address that you used at checkout. The portal lists your license keys. You can also recover the key from the Dodo customer portal, linked in **Settings → Premium Themes**.

## Troubleshooting

### "License activation limit has been reached"

All activation slots for your key are in use. Deactivate an old activation from the other qui instance (**Settings → Premium Themes → Remove**). If that server is gone, use [licenses.getqui.com](https://licenses.getqui.com/).

### "This license was activated on a different machine"

This error appears when you copy the qui database from another server. The stored activation does not match this machine's identity. Click **Re-activate License**, or remove the license (**Settings → Premium Themes → Remove**) and enter the key again.

### "Unable to reach the license service"

The license service did not respond. Wait a moment, then try again. If the error persists, make sure that your server has outbound network access.
