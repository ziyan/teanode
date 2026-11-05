package models

import "testing"

func TestShoppingItemSimilarity(test *testing.T) {
	test.Parallel()

	matching := []struct{ typed, description string }{
		{"milk", "Milk"},
		{"milk", "Whole milk"},
		{"MILK!", "2% milk"},
		{"eggs", "Egg"},
		{"tomato", "Tomatoes"},
		{"berry", "Strawberries, berries"},
		{"tom", "Tomatoes"},
		{"bannana", "Bananas"},
		{"brocolli", "Broccoli"},
		{"ice cream", "Icecream"},
		{"chocolate milk", "Milk chocolate"},
		{"牛奶", "全脂牛奶"},
		{"りんご", "青森りんご"},
	}
	for _, each := range matching {
		if score := ShoppingItemSimilarity(each.typed, each.description); score <= 0 {
			test.Errorf("%q should be similar to %q", each.typed, each.description)
		}
	}

	different := []struct{ typed, description string }{
		{"milk", "Bread"},
		{"to", "Tomatoes"},
		{"of the", "Bag of the rice"},
		{"12", "12 eggs"},
		{"ca", "Carrots"},
		{"bread", "tread"},
		{"", "Milk"},
		{"牛", "全脂牛奶"},
		{"牛奶", "面包"},
	}
	for _, each := range different {
		if score := ShoppingItemSimilarity(each.typed, each.description); score != 0 {
			test.Errorf("%q should not be similar to %q: %v", each.typed, each.description, score)
		}
	}

	if same, other := ShoppingItemSimilarity("milk", "Milk"), ShoppingItemSimilarity("milk", "Whole milk"); same != 1 || other >= same {
		test.Errorf("the same words rank first: %v, then %v", same, other)
	}
	if same, shuffled := ShoppingItemSimilarity("green apples", "Green Apple"), ShoppingItemSimilarity("green apples", "Apple, green"); same != 1 || shuffled != 1 {
		test.Errorf("order and plural do not matter: %v and %v", same, shuffled)
	}
	if whole, begun := ShoppingItemSimilarity("tomato", "Tomato paste"), ShoppingItemSimilarity("tom", "Tomato paste"); begun >= whole {
		test.Errorf("a whole word ranks above one only begun: %v and %v", whole, begun)
	}
}
