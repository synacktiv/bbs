Simple user systemd service that automatically reloads the bbs server if the configuration is modified.
The service units must be placed in `$HOME/.config/systemd/user/` and the bbs configuration files have to be placed in `$HOME/.config/bbs/`.

```bash
mkdir -p ~/.config/bbs/logs/ # create configuration tree
vim -p ~/.config/bbs/bbs.json # create configuration file
mkdir -p ~/.config/systemd/user/
loginctl enable-linger # make user systemd services persistent across logons
systemctl --user start bbs # To start bbs
systemctl --user stop bbs # To stop bbs
systemctl --user enable --now bbs.service # To autostart bbs server
```
