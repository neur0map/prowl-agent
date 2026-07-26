local terminal = 'kitty'

awful.key({ 'Mod4' }, 'Return', function()
  awful.spawn(terminal)
end)
