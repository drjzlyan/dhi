import unittest

from pricing import apply_discount


class ApplyDiscountTest(unittest.TestCase):
    def test_whole(self):
        self.assertEqual(apply_discount(1000, 10), 900)

    def test_rounds_half_up(self):
        # 1999 * 0.85 = 1699.15 -> 1699; 2999 * 0.85 = 2549.15 -> 2549
        self.assertEqual(apply_discount(1999, 15), 1699)
        self.assertEqual(apply_discount(2999, 15), 2549)
        # 50 * 0.99 = 49.5 -> 50 (half up)
        self.assertEqual(apply_discount(50, 1), 50)

    def test_bounds(self):
        with self.assertRaises(ValueError):
            apply_discount(100, 101)


if __name__ == "__main__":
    unittest.main()
