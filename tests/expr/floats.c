#include <stdio.h>
#include <string.h>
#include <math.h>

/* Floating point where the details bite: the conversions in both
   directions, float vs double width, printf's own conversions, and the
   comparisons NaN makes false. */

static double id(double x) { return x; }
static float idf(float x) { return x; }

int main(void) {
    char buf[64];

    /* float is 24 bits of significand; double is 53. The difference is
       observable, so the arithmetic really has to be done at each width. */
    float f = 0.1f;
    double d = 0.1;
    if ((double)f == d) return 1;
    if (f != 0.1f) return 2;
    if (idf(16777216.0f) + 1.0f != 16777216.0f) return 3;
    if (id(16777216.0) + 1.0 != 16777217.0) return 4;

    /* Conversion to an integer truncates toward zero, both signs. */
    if ((int)3.99 != 3) return 5;
    if ((int)-3.99 != -3) return 6;
    if ((long long)1e18 != 1000000000000000000LL) return 7;
    if ((unsigned)4294967295.0 != 4294967295u) return 8;

    /* Integer to floating and back. */
    if ((double)7 / 2 != 3.5) return 9;
    if (7 / 2 != 3) return 10;
    if ((double)(-7) / 2 != -3.5) return 11;
    long long big = 9007199254740993LL;      /* 2^53 + 1 */
    if ((long long)(double)big == big) return 12;

    /* Usual arithmetic conversions promote the int operand. */
    int i = 3;
    if (i / 2.0 != 1.5) return 13;
    if (2.0 * i != 6.0) return 14;

    /* NaN compares false against everything, itself included. */
    double nan = 0.0 / 0.0;
    if (nan == nan) return 15;
    if (!(nan != nan)) return 16;
    if (nan < 1.0 || nan > 1.0 || nan <= 1.0 || nan >= 1.0) return 17;

    /* Signed zero and infinity behave as IEEE says. */
    double zero = 0.0, negzero = -0.0;
    if (zero != negzero) return 18;
    if (1.0 / zero <= 0.0) return 19;
    if (1.0 / negzero >= 0.0) return 20;

    /* printf carries doubles through the variadic promotion. */
    snprintf(buf, sizeof buf, "%.2f", 3.14159);
    if (strcmp(buf, "3.14") != 0) return 21;
    snprintf(buf, sizeof buf, "%g", 0.5);
    if (strcmp(buf, "0.5") != 0) return 22;
    snprintf(buf, sizeof buf, "%.3e", 12345.678);
    if (strcmp(buf, "1.235e+04") != 0) return 23;
    snprintf(buf, sizeof buf, "%f", (double)idf(2.5f));
    if (strcmp(buf, "2.500000") != 0) return 24;

    /* A float argument to a variadic function is promoted to double. */
    snprintf(buf, sizeof buf, "%.1f|%.1f", 1.5f, 2.5);
    if (strcmp(buf, "1.5|2.5") != 0) return 25;

    /* libm, called with the right ABI. */
    if (sqrt(16.0) != 4.0) return 26;
    if (fabs(-2.5) != 2.5) return 27;
    if (floor(-1.5) != -2.0) return 28;
    if (ceil(-1.5) != -1.0) return 29;
    if (fmod(7.0, 3.0) != 1.0) return 30;
    if (ldexp(1.0, 10) != 1024.0) return 31;

    /* Assignment narrows: a double expression stored into a float is
       rounded to float, and the result is a float value from then on. */
    float narrowed = 3.14f + 2.718;      /* done at double, stored as float */
    if (narrowed < 5.85f || narrowed > 5.86f) return 34;
    if ((int)narrowed != 5) return 35;
    if ((float)(1.0 / 3.0) == 1.0 / 3.0) return 36;

    /* long double is a distinct type; on this target it is wider. */
    long double ld = 1.0L / 3.0L;
    if ((double)ld == 0.0) return 32;
    if (sizeof(long double) < sizeof(double)) return 33;

    printf("Floats OK\n");
    return 0;
}
