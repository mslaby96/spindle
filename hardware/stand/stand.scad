// Spindle "now playing" stand: a small ledge that holds a record sleeve leaning back,
// with an RC522 reader inside the backrest and a Pico W in the base.
// All dimensions in millimetres. Render a part with: openscad -D 'part="body"' -o body.stl stand.scad

part = "preview"; // "body", "cover" or "preview"

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
rc_depth = 8;           // board plus components
front_wall = 2.5;       // between the sleeve and the reader antenna
tag_height = 45;        // sticker centre above the bottom edge of the sleeve
antenna_from_top = 20;  // antenna centre measured from the top edge of the board

/* [Label] */
label = "Now Playing";
label_size = 8;
label_depth = 1;
label_font = "Liberation Sans:style=Bold";

/* [Electronics bay] */
shell = 3.5;
pico_l = 51.5;
pico_w = 21.5;
usb_w = 12;
usb_h = 8;
led_hole_d = 5;

/* [Cover] */
cover_t = 3;
screw_d = 2.2;          // for M2.5 self-tapping screws
boss_d = 7;
foot_d = 12;
foot_h = 3;
head_d = 5.5;           // pan head of the screw, sunk into the foot
head_h = 2.5;
pico_recess = 1;

$fn = 48;
eps = 0.01;

module rounded_box(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r], z = [r, size.z - r])
            translate([x, y, z]) sphere(r, $fn = 24);
}

module rounded_plate(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r])
            translate([x, y, 0]) cylinder(r = r, h = size.z);
}

slot_front = lip_t;
rest_front = lip_t + slot_w;
rest_len = rest_height / cos(lean);
board_top = tag_height + antenna_from_top;
board_bottom = board_top - rc_h;
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
    rounded_box([width, depth, base_h], corner_r);
}

module lip() {
    translate([0, 0, base_h - corner_r])
        rounded_box([width, lip_t, lip_h + corner_r], min(corner_r, lip_t / 2 - eps));
}

module rest() {
    in_rest()
        translate([0, 0, -corner_r])
            rounded_box([rest_width, rest_t, rest_len + corner_r], corner_r);
}

module reader_pocket() {
    in_rest() {
        translate([(rest_width - rc_w) / 2, front_wall, board_bottom])
            cube([rc_w, rest_t, rc_h]);
        // pin header and wires drop down into the base
        translate([(rest_width - rc_w) / 2, front_wall, -base_h])
            cube([rc_w, rc_depth, board_bottom + base_h + eps]);
    }
}

module bay() {
    translate([shell, shell, -eps])
        cube([width - 2 * shell, depth - 2 * shell, base_h - shell]);
}

module usb_opening() {
    translate([(width - usb_w) / 2, depth - shell - 1, -eps])
        cube([usb_w, shell + 2, usb_h]);
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
        // keep the backrest from poking below the base
        translate([-1, -1, -50]) cube([width + 2, depth + 2, 50]);
    }
    intersection() {
        bosses();
        base();
    }
}

// Printed upside down: the flat top on the bed, the feet facing up.
module cover() {
    difference() {
        union() {
            rounded_plate([width, depth, cover_t], corner_r);
            for (p = boss_positions)
                translate([p.x, p.y, -foot_h]) cylinder(d = foot_d, h = foot_h + eps);
        }
        for (p = boss_positions)
            translate([p.x, p.y, -foot_h - eps]) {
                cylinder(d = screw_d + 0.6, h = foot_h + cover_t + 2 * eps);
                cylinder(d = head_d, h = head_h);
            }
        // shallow bed that keeps the Pico W lined up with the USB opening
        translate([pico_x - 0.5, pico_y - 0.5, cover_t - pico_recess])
            cube([pico_w + 1, pico_l + 1, pico_recess + eps]);
    }
}

if (part == "body") body();
else if (part == "cover") cover();
else {
    body();
    translate([0, 0, -40]) cover();
    %in_rest() translate([(rest_width - 314) / 2, -4.5, 0]) cube([314, 4, 314]);
}
