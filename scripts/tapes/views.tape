# Every view: Editor, Workspace board and channels, Ideator, Reviewer — rendered by scripts/record.sh ({{OUT}}, {{WORK}} are filled in).
Output {{OUT}}/views.gif
Output {{OUT}}/views.mp4
Output {{WORK}}/views.txt

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

# The Editor opens on its start page; open api/main.go from the tree.
Sleep 2.5s
Enter
Sleep 600ms
Down
Down
Down
Sleep 600ms
Enter
Sleep 3s
# alt+2: the Workspace board; ] walks its sections.
Alt+2
Sleep 3s
Type "]"
Sleep 3s
Type "["
Type "["
Sleep 3s
# alt+3: the Ideator; alt+4: the Reviewer with a diff.
Alt+3
Sleep 3s
Alt+4
Sleep 1.5s
Enter
Sleep 1s
Enter
Sleep 3.5s
# Back to the Editor.
Alt+1
Sleep 2.5s
