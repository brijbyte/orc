#ifndef ORC_ANSI_H
#define ORC_ANSI_H

/* Named ANSI sequences (SGR + OSC 8), the one place escapes are spelled out.
 * No vendorable C styling library exists; terminfo/ncurses is a heavy system
 * dep and lacks strikethrough and hyperlinks, so we name the codes here. */

#define ANSI_RESET "\x1b[0m"
#define ANSI_BOLD "\x1b[1m"
#define ANSI_DIM "\x1b[2m"

/* SGR codes for composed sequences: "\x1b[" code (";" code)* "m". */
enum {
    SGR_BOLD = 1,
    SGR_DIM = 2,
    SGR_ITALIC = 3,
    SGR_UNDERLINE = 4,
    SGR_STRIKE = 9,
    SGR_FG_CYAN = 36,
    SGR_FG_BRIGHT_BLUE = 94,
    SGR_FG_BRIGHT_MAGENTA = 95,
    SGR_FG_BRIGHT_CYAN = 96,
};

/* OSC 8 hyperlink: OSC8_OPEN <url> OSC8_ST <text> OSC8_CLOSE */
#define OSC8_OPEN "\x1b]8;;"
#define OSC8_ST "\x1b\\"
#define OSC8_CLOSE "\x1b]8;;" "\x1b\\"

#endif
