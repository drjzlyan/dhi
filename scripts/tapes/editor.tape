# The Editor: files, a split, typing, search and the terminal — rendered by scripts/record.sh ({{OUT}}, {{WORK}} are filled in).
Output "{{OUT}}/editor.gif"
Output "{{OUT}}/editor.mp4"
Output "{{WORK}}/editor.txt"

Set Shell bash
Set FontSize 15
Set Width 1440
Set Height 860
Set Padding 16
Set Framerate 20
Set TypingSpeed 80ms
Set WindowBar Colorful
Set BorderRadius 10
Set Theme { "name": "dhi-dusk", "background": "#0D0E16", "foreground": "#ECEAF4", "cursor": "#FF9F43", "selection": "#232842", "black": "#0D0E16", "red": "#FF7B7B", "green": "#7EE081", "yellow": "#F5D547", "blue": "#A3A4FF", "magenta": "#E879F9", "cyan": "#2DD4BF", "white": "#ECEAF4", "brightBlack": "#8D92AB", "brightRed": "#FF7B7B", "brightGreen": "#7EE081", "brightYellow": "#F5D547", "brightBlue": "#A3A4FF", "brightMagenta": "#E879F9", "brightCyan": "#2DD4BF", "brightWhite": "#FFFFFF" }

Hide
Type ". '{{WORK}}/env-demo.sh' && cd '{{WORK}}/acme' && clear && dhi"
Enter
Sleep 4s
Show

Sleep 2s
Enter
Down
Down
Down
Enter
Sleep 2s
# Insert a line in vim style, then back to normal mode.
Type "o"
Type "	// Rate limits apply per client."
Escape
Sleep 1.5s
# Split the pane and open the README beside it.
Ctrl+W
Type "v"
Sleep 1.5s
Escape
Type "/"
Sleep 600ms
Type "readme"
Sleep 1s
Enter
Sleep 2.5s
# The terminal drawer runs real commands.
Ctrl+T
Sleep 1.5s
Type "git log --oneline -3"
Enter
Sleep 2.5s
Ctrl+T
Sleep 2s
