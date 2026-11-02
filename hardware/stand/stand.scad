// Copyright 2026 Maciej Słaby
// SPDX-License-Identifier: CERN-OHL-S-2.0
// Source location: https://github.com/mslaby96/spindle
//
// Spindle "now playing" stand: a small ledge that holds a record sleeve leaning back,
// with an RC522 reader inside the backrest and a Pico W in the base.
// All dimensions in millimetres. Render a part with: openscad -D 'part="body"' -o body.stl stand.scad

part = "preview"; // "body", "cover", "foot", "clip" or "preview"

/* [Stand] */
width = 140;
depth = 100;
base_h = 24;
corner_r = 6;
lean = 15;              // backrest tilt from vertical, degrees
rest_width = 110;
rest_height = 120;      // vertical height of the backrest above the base
rest_t = 14;
lip_h = 14;             // front lip height above the base
lip_t = 7;
slot_w = 10;            // room for a gatefold sleeve in its outer sleeve

/* [Reader] */
pocket_w = 40.5;        // the pocket in the printed body; the board is a little smaller
pocket_h = 60.5;
board_w = 39.4;         // the common blue RFID-RC522 module
board_h = 59.8;
board_holes = 24.9;     // between the two mounting holes at the antenna end
board_hole_from_top = 7.8;
board_hole_d = 3;
rc_depth = 8;           // board plus components; the pin header must point down, not out of the board
fit = 0.4;              // clearance around the board
leg_w = 4;              // legs of the spacer that keeps the board up
pcb_t = 1.6;
spacer_lip = 4;         // how far the spacer's lips reach up beside the board
back_lip = 1;           // behind the board's bottom edge, low enough to pass under the crystal and the LED
pcb_slot = 0.3;         // room around the board's thickness behind that lip
spacer_play = 0.3;      // extra room around the board, since the screws decide where the spacer ends up
side_lip = 1.5;
front_wall = 2.5;       // between the sleeve and the reader antenna
header_band = 12;       // strip at the bottom of the board with the straight pin header, kept inside the base
antenna_from_top = 20;  // antenna centre measured from the top edge of the board

/* [Reader clip] */
clip_h = 8;             // along the board
clip_t = 1;             // the spring's thickness
clip_preload = 0.8;     // how far the spring is pressed in when the board is home

/* [Label] */
label = "Now Playing";
label_size = 8;
label_depth = 1;
label_font = "Liberation Sans:style=Bold";

/* [Reader mark] */
mark_depth = 0.6;
mark_stroke = 1.8;

/* [Electronics bay] */
shell = 3.5;
pico_l = 51.5;
pico_w = 21.5;
usb_w = 12;
usb_h = 8;
led_hole_d = 5;

/* [Cover] */
cover_t = 3;
screw_d = 2.5;          // M3 x 20 machine screws cut their own thread in the bosses
screw_clear = 3.4;
boss_d = 8;
foot_d = 16;            // wide enough to hold the cover up around each boss
foot_h = 4.5;
head_d = 6.5;           // pan, socket or countersunk M3 head, sunk into the foot
head_h = 3.2;
tray_h = 6;             // extra room under the base for wires and a breadboard
tray_lip = 2;           // how far the tray's rim reaches up into the base, so the base sits on it in place
tray_lip_t = 1.5;
cover_play = 0.3;

$fn = 48;
eps = 0.01;

module rounded_box(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r], z = [r, size.z - r])
            translate([x, y, z]) sphere(r, $fn = 24);
}

// Rounded on top, straight down to a flat bottom so nothing overhangs the print bed.
module flat_bottom_box(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r]) {
            translate([x, y, 0]) cylinder(r = r, h = size.z - r);
            translate([x, y, size.z - r]) sphere(r, $fn = 24);
        }
}

module rounded_plate(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r])
            translate([x, y, 0]) cylinder(r = r, h = size.z);
}

slot_front = lip_t;
rest_front = lip_t + slot_w;
rest_len = rest_height / cos(lean);
// The header band hangs below the ceiling of the base so the pins and plugs can stick out backwards.
board_bottom = -(shell + header_band) / cos(lean);
board_top = board_bottom + pocket_h;
// the board pushed up against the top of the pocket by the spacer
board_low = board_top - board_h;
tag_height = board_top - antenna_from_top; // sticker centre above the bottom edge of the sleeve
echo(str("Sticker centre: ", round(tag_height), " mm above the bottom edge of the sleeve"));
boss_inset = corner_r + boss_d / 2;
boss_positions = [
    [boss_inset, boss_inset], [width - boss_inset, boss_inset],
    [boss_inset, depth - boss_inset], [width - boss_inset, depth - boss_inset]
];
// the Pico lies front to back with its USB end against the back wall
pico_x = (width - pico_w) / 2;
pico_y = depth - shell - pico_l - 0.5;

// Places children in the backrest's own frame: x across, y into the rest from its front face, z up the slope.
module in_rest() {
    translate([(width - rest_width) / 2, rest_front, base_h])
        rotate([-lean, 0, 0])
            children();
}

module base() {
    flat_bottom_box([width, depth, base_h], corner_r);
}

// reaches well below the base's rounded top edge so the front reads as one flat face
module lip() {
    translate([0, 0, base_h - 2 * corner_r])
        rounded_box([width, lip_t, lip_h + 2 * corner_r], min(corner_r, lip_t / 2 - eps));
}

module rest() {
    in_rest()
        translate([0, 0, -corner_r])
            rounded_box([rest_width, rest_t, rest_len + corner_r], corner_r);
}

// The board slides in from below, up along the backrest, and the spacer standing on the cover holds it there.
module reader_pocket() {
    in_rest()
        translate([(rest_width - pocket_w - fit) / 2, front_wall, -1.5 * base_h])
            cube([pocket_w + fit, rc_depth, board_top + 1.5 * base_h]);
}

// Two legs stand on the cover and push the board against the top of its pocket, so it can't slide once the cover is
// on. A low lip behind the board's bottom edge keeps it against the front wall, and lips beside it centre it.
module spacer() {
    bx0 = (rest_width - board_w) / 2;
    bx1 = bx0 + board_w;
    y0 = front_wall + fit / 2;
    y1 = front_wall + rc_depth - fit / 2;
    top = board_low - 0.1;
    lip_top = board_low + spacer_lip;
    intersection() {
        in_rest()
            for (side = [[bx0 - side_lip - spacer_play, bx0 + leg_w, bx0 - side_lip, bx0],
                         [bx1 - leg_w, bx1 + side_lip + spacer_play, bx1, bx1 + side_lip]]) {
                translate([side[0], y0, -1.5 * base_h])
                    cube([side[1] - side[0], y1 - y0, 1.5 * base_h + top]);
                // behind the board
                translate([side[0], front_wall + pcb_t + pcb_slot, top - eps])
                    cube([side[1] - side[0], y1 - front_wall - pcb_t - pcb_slot, back_lip + eps]);
                // beside the board
                translate([side[2] + (side[2] < bx0 ? -spacer_play : spacer_play), y0, top - eps])
                    cube([side[3] - side[2], y1 - y0, lip_top - top]);
            }
        translate([0, 0, -tray_h - eps]) cube([width, depth, base_h - shell + tray_h]);
    }
    hull()
        intersection() {
            in_rest()
                translate([bx0 - side_lip - spacer_play, y0, -1.5 * base_h])
                    cube([board_w + 2 * (side_lip + spacer_play), y1 - y0, 1.5 * base_h]);
            translate([0, 0, -tray_h - eps]) cube([width, depth, 2]);
        }
}

module bay() {
    translate([shell, shell, -eps])
        cube([width - 2 * shell, depth - 2 * shell, base_h - shell]);
}

// A 45 degree roof over the opening prints without support.
module usb_opening() {
    translate([(width - usb_w) / 2, depth - shell - 1, -eps])
        rotate([-90, 0, 0])
            mirror([0, 1, 0])
                linear_extrude(shell + 2)
                    polygon([[0, 0], [usb_w, 0], [usb_w, usb_h], [usb_w / 2, usb_h + usb_w / 2], [0, usb_h]]);
}

// the Pico W's LED sits near its USB end
module led_hole() {
    translate([width / 2, depth - shell - 8, base_h - shell - 1])
        cylinder(d = led_hole_d, h = shell + 2);
}

module centre_mark() {
    translate([width / 2, -eps, base_h + lip_h])
        rotate([-90, 0, 0])
            cylinder(r1 = 2, r2 = 2, h = lip_t + 2 * eps, $fn = 3);
}

// The letters run into the face at 45 degrees downwards, so their tops print without support.
module label_cut() {
    minkowski() {
        translate([width / 2, -eps, (base_h + lip_h) / 2])
            rotate([90, 0, 0])
                linear_extrude(1)
                    text(label, size = label_size, font = label_font, halign = "center", valign = "center");
        hull() {
            cube(eps);
            translate([0, label_depth, -label_depth]) cube(eps);
        }
    }
}

module arc(r, w, half_angle) {
    intersection() {
        difference() {
            circle(r + w / 2);
            circle(r - w / 2);
        }
        polygon([[0, 0], [2 * r * cos(half_angle), 2 * r * sin(half_angle)],
                 [2 * r, 0], [2 * r * cos(half_angle), -2 * r * sin(half_angle)]]);
    }
}

// contactless symbol over the antenna, so it's clear where the sticker should land
module reader_mark() {
    in_rest()
        translate([rest_width / 2 - 8, mark_depth, tag_height])
            rotate([90, 0, 0])
                linear_extrude(mark_depth + eps) {
                    circle(d = mark_stroke * 1.6);
                    for (r = [6, 10.5, 15]) arc(r, mark_stroke, 42);
                }
}

module bosses() {
    for (p = boss_positions)
        translate([p.x, p.y, 0])
            difference() {
                cylinder(d = boss_d, h = base_h - shell + eps);
                translate([0, 0, -eps]) cylinder(d = screw_d, h = base_h - shell - 2);
                // pointed end, so the hole needs no support inside
                translate([0, 0, base_h - shell - 2 - eps]) cylinder(d1 = screw_d, d2 = 0, h = screw_d / 2);
            }
}

module body() {
    difference() {
        union() {
            base();
            lip();
            rest();
        }
        bay();
        reader_pocket();
        usb_opening();
        led_hole();
        centre_mark();
        label_cut();
        reader_mark();
        // keep the backrest from poking below the base
        translate([-1, -1, -50]) cube([width + 2, depth + 2, 50]);
    }
    intersection() {
        bosses();
        base();
    }
}

// A tray under the base, with the base's outline, that adds tray_h of room inside. Its rim reaches a little way into
// the base so the two line up, posts under the bosses take the screws' pull, and the reader spacer stands on its floor.
// Drawn where it sits under the body; the exported part is moved up onto the bed.
module tray() {
    floor_z = -tray_h - cover_t;
    inner = [width - 2 * (shell + cover_play), depth - 2 * (shell + cover_play)];
    difference() {
        union() {
            translate([0, 0, floor_z]) difference() {
                rounded_plate([width, depth, tray_h + cover_t], corner_r);
                // walls as thick as the base's plus the rim, so the rim stands on them
                wall = shell + cover_play + tray_lip_t;
                translate([wall, wall, cover_t]) cube([width - 2 * wall, depth - 2 * wall, tray_h + eps]);
            }
            translate([shell + cover_play, shell + cover_play, -eps]) difference() {
                cube([inner.x, inner.y, tray_lip + eps]);
                translate([tray_lip_t, tray_lip_t, -eps]) cube([inner.x - 2 * tray_lip_t, inner.y - 2 * tray_lip_t, tray_lip + 3 * eps]);
            }
            for (p = boss_positions) translate([p.x, p.y, -tray_h - eps]) cylinder(d = boss_d, h = tray_h + eps);
            spacer();
        }
        for (p = boss_positions)
            translate([p.x, p.y, floor_z - eps]) cylinder(d = screw_clear, h = tray_h + cover_t + 2 * eps);
    }
}

module cover() {
    translate([0, 0, tray_h + cover_t]) tray();
}

// One under each corner. The screw goes through it into the boss, and its rim holds up the cover and the base wall.
module foot() {
    difference() {
        cylinder(d = foot_d, h = foot_h);
        translate([0, 0, -eps]) cylinder(d = screw_clear, h = foot_h + 2 * eps);
        translate([0, 0, -eps]) cylinder(d = head_d, h = head_h);
    }
}

// Clips onto the two mounting holes at the board's antenna end, on the side with the parts. Its bow presses on the
// back of the pocket, so the antenna stays against the front wall however the stand leans. Printed standing up, as
// exported, with the pegs pointing sideways.
module clip() {
    room = rc_depth - pcb_t;
    reach = room + clip_preload;
    pad = 5;
    span = board_holes + pad;
    peg_d = board_hole_d - 0.4;
    linear_extrude(clip_h) {
        for (x = [0, board_holes]) translate([x - pad / 2, 0]) square([pad, clip_t + 0.2]);
        // the bow, from one pad to the other
        intersection() {
            translate([board_holes / 2, 0]) scale([span / 2, reach]) difference() {
                circle(1, $fn = 96);
                circle(1 - clip_t / reach, $fn = 96);
            }
            translate([-pad / 2, 0]) square([span, reach]);
        }
    }
    for (x = [0, board_holes])
        translate([x, eps, clip_h / 2]) rotate([90, 0, 0]) cylinder(d = peg_d, h = pcb_t + eps);
}

if (part == "body") body();
else if (part == "cover") cover();
else if (part == "foot") foot();
else if (part == "clip") clip();
else {
    body();
    color("orange") translate([0, 0, -30]) tray();
    for (p = boss_positions) translate([p.x, p.y, -50 - foot_h]) foot();
    %in_rest() translate([(rest_width - 314) / 2, -4.5, 0]) cube([314, 4, 314]);
}
