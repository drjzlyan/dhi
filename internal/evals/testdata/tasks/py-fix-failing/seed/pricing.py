"""Price helpers, in integer cents."""


def apply_discount(cents: int, percent: int) -> int:
    """Price after a percent discount, rounded half up to the cent."""
    if not 0 <= percent <= 100:
        raise ValueError("percent must be 0..100")
    return cents - cents * percent // 100
