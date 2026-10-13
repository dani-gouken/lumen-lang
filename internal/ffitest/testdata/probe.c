// A small C library the tests of the functions of C libraries call: one
// function for each way a value goes to C and comes back.

#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

typedef struct { uint8_t r, g, b, a; } Color;
typedef struct { float x, y; } Vec2;
typedef struct { double x, y; } Point;
typedef struct { float x, y, width, height; } Rect;
// Structs inside a struct, and more floats than go in registers
typedef struct { Vec2 offset; Vec2 target; float rotation; float zoom; } Camera;
// A struct whose end is rounded up, inside another: flag is at 16, not 9
typedef struct { double weight; uint8_t tag; } Inner;
typedef struct { Inner inner; uint8_t flag; int32_t count; } Padded;
// An address among numbers, 24 bytes: given by its own address
typedef struct { void *data; int32_t width, height, format; } Image;
// A float and a whole number in 8 bytes: one word
typedef struct { float ratio; int32_t count; } Pair;
typedef struct {
    int8_t a; int16_t b; int32_t c; int64_t d;
    uint8_t e; uint16_t f; uint32_t g; uint64_t h;
    float i; double j; bool k;
} Mixed;

int32_t probe_add(int32_t a, int32_t b) { return a + b; }
bool probe_not(bool value) { return !value; }

double probe_scalars(int8_t a, int16_t b, int32_t c, int64_t d,
                     uint8_t e, uint16_t f, uint32_t g, uint64_t h,
                     float i, double j, bool k) {
    return a + b + c + (double)d + e + f + g + (double)h + i + j + (k ? 1000 : 0);
}

int8_t probe_i8(int8_t n) { return n; }
uint8_t probe_u8(uint8_t n) { return n; }
int16_t probe_i16(int16_t n) { return n; }
uint16_t probe_u16(uint16_t n) { return n; }
uint32_t probe_u32(uint32_t n) { return n; }
int64_t probe_i64(int64_t n) { return n; }
float probe_half(float n) { return n / 2; }

// More arguments than there are registers: the last ones go on the stack
int64_t probe_ten(int32_t a, int32_t b, int32_t c, int32_t d, int32_t e,
                  int32_t f, int32_t g, int32_t h, int32_t i, int32_t j) {
    return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f + 7 * g + 8 * h + 9 * i + 10 * (int64_t)j;
}
double probe_ten_floats(double a, double b, double c, double d, double e,
                        double f, double g, double h, double i, double j) {
    return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f + 7 * g + 8 * h + 9 * i + 10 * j;
}

uint32_t probe_color_pack(Color c) {
    return ((uint32_t)c.r << 24) | ((uint32_t)c.g << 16) | ((uint32_t)c.b << 8) | c.a;
}
Color probe_color_unpack(uint32_t packed) {
    Color c = { packed >> 24, packed >> 16, packed >> 8, packed };
    return c;
}
Vec2 probe_vec_add(Vec2 a, Vec2 b) { Vec2 v = { a.x + b.x, a.y + b.y }; return v; }
Point probe_point_scale(Point p, double by) { Point q = { p.x * by, p.y * by }; return q; }
bool probe_rect_holds(Rect r, Vec2 p) {
    return p.x >= r.x && p.x < r.x + r.width && p.y >= r.y && p.y < r.y + r.height;
}
Rect probe_rect_grow(Rect r, float by) {
    Rect out = { r.x - by, r.y - by, r.width + 2 * by, r.height + 2 * by };
    return out;
}
Camera probe_camera_zoom(Camera c, float by) { c.zoom *= by; c.target.x += 1; c.offset.y -= 1; return c; }
float probe_camera_sum(Camera c) {
    return c.offset.x + c.offset.y + c.target.x + c.target.y + c.rotation + c.zoom;
}
Padded probe_padded_bump(Padded p) {
    p.inner.weight += 0.5; p.inner.tag += 1; p.flag += 2; p.count *= 2;
    return p;
}
double probe_padded_sum(Padded p) { return p.inner.weight + p.inner.tag + p.flag + p.count; }
Pair probe_pair_swap(Pair p) { Pair q = { (float)p.count, (int32_t)p.ratio }; return q; }
float probe_pair_sum(Pair p) { return p.ratio + p.count; }
Mixed probe_mixed_bump(Mixed m) {
    m.a += 1; m.b += 1; m.c += 1; m.d += 1; m.e += 1; m.f += 1; m.g += 1; m.h += 1;
    m.i += 1; m.j += 1; m.k = !m.k;
    return m;
}
// Structs between numbers, and enough of them to run out of registers
double probe_crowd(int32_t n, Color a, Vec2 b, Rect c, double d, Color e, Image f, Pair g, Vec2 h, Rect i) {
    return n + a.r + b.x + b.y + c.width + d + e.a + f.width + f.height + g.count + g.ratio
        + h.x + h.y + i.x + i.height + ((uint8_t *)f.data)[1];
}

static uint8_t pixels[16] = { 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160 };
Image probe_image(int32_t width, int32_t height) {
    Image image = { pixels, width, height, 7 };
    return image;
}
int32_t probe_image_pixel(Image image, int32_t at) { return ((uint8_t *)image.data)[at]; }

static char said[256];
const char *probe_greet(const char *name) {
    snprintf(said, sizeof said, "hello, %s!", name);
    return said;
}
int32_t probe_bytes(const char *text) { return (int32_t)strlen(text); }
const char *probe_nothing(void) { return 0; }

void probe_squares(int32_t *out, int32_t count) {
    for (int32_t i = 0; i < count; i++) out[i] = i * i;
}
int64_t probe_sum(const int32_t *values, int32_t count) {
    int64_t sum = 0;
    for (int32_t i = 0; i < count; i++) sum += values[i];
    return sum;
}
float probe_mean(const float *values, int32_t count) {
    float sum = 0;
    for (int32_t i = 0; i < count; i++) sum += values[i];
    return count ? sum / count : 0;
}
// A struct filled through its address
void probe_origin(Vec2 *out, Rect r) { out->x = r.x + r.width / 2; out->y = r.y + r.height / 2; }

static int32_t counter;
void probe_count(void) { counter++; }
void probe_reset(void) { counter = 0; }
int32_t probe_counted(void) { return counter; }

// A text the library keeps the address of
static const char *kept;
void probe_keep(const char *text) { kept = text; }
const char *probe_kept(void) { return kept; }

int64_t probe_huge(void) { return INT64_MAX; }
uint64_t probe_huge_unsigned(void) { return UINT64_MAX; }
