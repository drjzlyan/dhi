# Settings: switch the theme live, toggle motion, type a value — rendered by scripts/record.sh ({{OUT}}, {{WORK}} are filled in).
Output "{{OUT}}/settings.gif"
Output "{{OUT}}/settings.mp4"
Output "{{WORK}}/settings.txt"

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
Type "5"
Sleep 2.5s
# Down to the theme row; enter cycles it, live.
Type "jjjjjjjjj"
Sleep 1s
Enter
Sleep 2.5s
Enter
Sleep 2.5s
Enter
Sleep 2s
# Reduce motion, then the tab width as a typed value.
Type "j"
Enter
Sleep 1.5s
Type "j"
Type "e"
Sleep 600ms
Backspace
Type "2"
Sleep 800ms
Enter
Sleep 2.5s
