# Recorded by demo/record.sh on a synthetic demo home; @HOME@, @INDEX@ and @BIN@ are filled in there.
Output assets/demo.gif

Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 17
Set LetterSpacing 0
Set LineHeight 1.2
Set Width 1280
Set Height 780
Set Padding 22
Set TypingSpeed 70ms
Set Theme {"name":"kioku","black":"#1c1d22","red":"#f6857a","green":"#8fc990","yellow":"#ddae6c","blue":"#c0d3eb","magenta":"#c0d3eb","cyan":"#76c7cc","white":"#d4d6dc","brightBlack":"#6f7e8d","brightRed":"#f6857a","brightGreen":"#8fc990","brightYellow":"#ddae6c","brightBlue":"#c0d3eb","brightMagenta":"#c0d3eb","brightCyan":"#76c7cc","brightWhite":"#ffffff","background":"#1c1d22","foreground":"#d4d6dc","selection":"#293647","cursor":"#d4d6dc"}

Hide
Type "export HOME=@HOME@ KIOKU_INDEX=@INDEX@ KIOKU_THEME=dark KIOKU_EDITOR=zed PATH=@BIN@:$PATH PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 600ms
Type "kioku checkout" Sleep 400ms Enter
Sleep 2s

# Move through results, then jump between hits in the transcript.
Escape
Sleep 500ms
Down
Sleep 700ms
Down
Sleep 900ms
Type "n"
Sleep 900ms
Type "n"
Sleep 1s

# Full transcript, then back.
Type "v"
Sleep 1500ms
Type "v"
Sleep 600ms

# CJK search and a highlight.
Type "/"
Ctrl+U
Type "結帳"
Sleep 1500ms
Escape
Sleep 700ms
Type "h"
Type "Pay"
Enter
Sleep 2s

# Agent marks and keys.
Type "?"
Sleep 2500ms
