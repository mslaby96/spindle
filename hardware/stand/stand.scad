// Spindle "now playing" stand: a small ledge that holds a record sleeve leaning back,
// with an RC522 reader inside the backrest and a Pico W in the base.
// All dimensions in millimetres. Render a part with: openscad -D 'part="body"' -o body.stl stand.scad

part = "preview"; // "body", "cover", "foot" or "preview"

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
rc_w = 40.5;
rc_h = 60.5;
rc_depth = 8;           // board plus components; the pin header must point down, not out of the board
fit = 0.4;              // clearance around the board
leg_w = 4;              // legs of the spacer that keeps the board up
pcb_t = 1.6;
spacer_lip = 4;         // how far the spacer's lips reach up beside and behind the board
spacer_play = 0.3;      // extra room around the board, since the screws decide where the spacer ends up
side_lip = 1.5;
front_wall = 2.5;       // between the sleeve and the reader antenna
header_band = 12;       // strip at the bottom of the board with the straight pin header, kept inside the base
antenna_from_top = 20;  // antenna centre measured from the top edge of the board

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
foot_d = 12;
foot_h = 4.5;
head_d = 6.5;           // pan, socket or countersunk M3 head, sunk into the foot
head_h = 3.2;
spigot_d = 7;           // centres a foot under its screw
spigot_h = 1;
pico_recess = 1;

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
board_top = board_bottom + rc_h;
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
        translate([(rest_width - rc_w - fit) / 2, front_wall, -1.5 * base_h])
            cube([rc_w + fit, rc_depth, board_top + 1.5 * base_h]);
}

// Two legs stand on the cover and carry the bottom edge of the board. A lip behind and beside the board on each leg
// lets the board itself hold the spacer in place; the cover holds it up once screwed on.
module spacer() {
    bx0 = (rest_width - rc_w) / 2;
    bx1 = bx0 + rc_w;
    y0 = front_wall + fit / 2;
    y1 = front_wall + rc_depth - fit / 2;
    top = board_bottom - 0.5;
    lip_top = board_bottom + spacer_lip;
    intersection() {
        in_rest()
            for (side = [[bx0 - side_lip - spacer_play, bx0 + leg_w, bx0 - side_lip, bx0],
                         [bx1 - leg_w, bx1 + side_lip + spacer_play, bx1, bx1 + side_lip]]) {
                translate([side[0], y0, -1.5 * base_h])
                    cube([side[1] - side[0], y1 - y0, 1.5 * base_h + top]);
                // behind the board
                translate([side[0], front_wall + pcb_t + fit + spacer_play, top - eps])
                    cube([side[1] - side[0], y1 - front_wall - pcb_t - fit - spacer_play, lip_top - top]);
                // beside the board
                translate([side[2] + (side[2] < bx0 ? -spacer_play : spacer_play), y0, top - eps])
                    cube([side[3] - side[2], y1 - y0, lip_top - top]);
            }
        cube([width, depth, base_h - shell]);
    }
    hull()
        intersection() {
            in_rest()
                translate([bx0 - side_lip - spacer_play, y0, -1.5 * base_h])
                    cube([rc_w + 2 * (side_lip + spacer_play), y1 - y0, 1.5 * base_h]);
            cube([width, depth, 2]);
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

module label_cut() {
    translate([width / 2, label_depth - eps, (base_h + lip_h) / 2])
        rotate([90, 0, 0])
            linear_extrude(label_depth + 1)
                text(label, size = label_size, font = label_font, halign = "center", valign = "center");
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

// Printed as it sits, outside face on the bed, with the reader spacer standing on it.
module cover() {
    difference() {
        union() {
            rounded_plate([width, depth, cover_t], corner_r);
            translate([0, 0, cover_t]) spacer();
        }
        for (p = boss_positions)
            translate([p.x, p.y, 0]) {
                translate([0, 0, -eps]) cylinder(d = screw_clear, h = cover_t + 2 * eps);
                translate([0, 0, -eps]) cylinder(d = spigot_d + 0.4, h = spigot_h + 0.2);
            }
        // shallow bed that keeps the Pico W lined up with the USB opening
        translate([pico_x - 0.5, pico_y - 0.5, cover_t - pico_recess])
            cube([pico_w + 1, pico_l + 1, pico_recess + eps]);
    }
}

// One under each corner, held by the same screw as the cover; the head sinks into the foot.
module foot() {
    difference() {
        union() {
            cylinder(d = foot_d, h = foot_h);
            translate([0, 0, foot_h - eps]) cylinder(d = spigot_d, h = spigot_h + eps);
        }
        translate([0, 0, -eps]) cylinder(d = screw_clear, h = foot_h + spigot_h + 2 * eps);
        translate([0, 0, -eps]) cylinder(d = head_d, h = head_h);
    }
}

if (part == "body") body();
else if (part == "cover") cover();
else if (part == "foot") foot();
else {
    body();
    color("orange") translate([0, 0, -30 - cover_t]) cover();
    for (p = boss_positions) translate([p.x, p.y, -50 - foot_h]) foot();
    %in_rest() translate([(rest_width - 314) / 2, -4.5, 0]) cube([314, 4, 314]);
}
