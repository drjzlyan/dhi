# Onboarding a fresh project: the Editor start page and its setup checklist — rendered by scripts/record.sh ({{OUT}}, {{WORK}} are filled in).
Output "{{OUT}}/onboarding.gif"
Output "{{OUT}}/onboarding.mp4"
Output "{{WORK}}/onboarding.txt"

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
Type ". '{{WORK}}/env-fresh.sh' && cd '{{WORK}}/shop' && clear && dhi"
Enter
Sleep 4s
Show

# The first screen: the Editor's start page, with the checklist.
Sleep 3s
# Create the workspace: pick both repos, enter — DHI relaunches into it.
Type "c"
Sleep 2.5s
Type "a"
Sleep 1.2s
Enter
Sleep 5s
# Describe the project: the draft comes from the repos; add a goal.
Type "c"
Sleep 2.5s
Tab
Tab
Sleep 600ms
Type "Ship the storefront beta in March"
Sleep 1.5s
Enter
Sleep 2s
# Git identity: found, use it.
Type "c"
Sleep 2.5s
Enter
Sleep 1.5s
# How you like to work: the defaults are fine.
Type "c"
Sleep 2.5s
Enter
Sleep 1.5s
# House rules: tick two suggestions.
Type "c"
Sleep 2s
Space
Down
Sleep 400ms
Space
Sleep 1.5s
Enter
Sleep 1.5s
# Branch and commit style: conventional commits, with a live preview.
Type "c"
Sleep 2s
Right
Sleep 2.5s
Enter
Sleep 2s
# Pick a team from the palette: the squad. DHI relaunches with it.
Ctrl+P
Sleep 800ms
Type "pick a team"
Sleep 800ms
Enter
Sleep 2s
Right
Sleep 2.5s
Enter
Sleep 5s
Sleep 3s
