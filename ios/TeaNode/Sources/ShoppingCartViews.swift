import PhotosUI
import SwiftUI
import TeaNodeKit

// Done shopping, and coming back from it: the cart to look over before it is
// bought, with the receipt to snap or a confirmed finish without one, and the
// row that resumes a trip finished by mistake.

/// ShoppingCartSheet is the person's cart at Done: what they put in it, each
/// with a check that takes it out again, about what it comes to, and the two
/// ways to finish. Snapping the receipt is the one prominent button; finishing
/// without one asks first, since the receipt is what remembers the prices.
/// Closing the sheet keeps shopping.
struct ShoppingCartSheet: View {
    /// Called with what to tell the person once the trip is finished.
    let onFinished: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var model: ShoppingCartModel
    @State private var pickedReceiptItems: [PhotosPickerItem] = []
    @State private var isTakingReceiptPicture = false
    @State private var isConfirmingWithoutReceipt = false

    init(connection: Connection, storeName: String, ownUserId: String?, shownItems: [ShoppingItem], onFinished: @escaping (String) -> Void) {
        self.onFinished = onFinished
        _model = State(
            initialValue: ShoppingCartModel(connection: connection, storeName: storeName, ownUserId: ownUserId, shownItems: shownItems))
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(model.items) { item in
                        ShoppingCartRow(item: item, showsListName: model.listCount > 1) {
                            Task { await model.takeOut(item) }
                        }
                    }
                } header: {
                    Text(verbatim: ShoppingWords.cartSummary(itemCount: model.items.count, total: model.total))
                        .textCase(nil)
                }
            }
            .overlay {
                if model.items.isEmpty {
                    ContentUnavailableView(
                        "Your cart is empty", systemImage: "cart",
                        description: Text("Check items off at the store to put them in your cart."))
                }
            }
            .navigationTitle("Your cart")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        dismiss()
                    } label: {
                        Label("Keep shopping", systemImage: "xmark")
                    }
                }
            }
            .safeAreaInset(edge: .bottom) { finishButtons }
            .task { await model.load() }
            .shoppingReceiptSource(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture) { file in
                Task { await finish(receipt: file) }
            }
            .toast(done: .constant(nil), problem: $model.problemText)
            .interactiveDismissDisabled(model.isWorking)
        }
        .presentationDetents([.large])
    }

    /// Snapping the receipt, then finishing without one, which asks first.
    private var finishButtons: some View {
        VStack(spacing: 12) {
            if let workingText = model.workingText {
                HStack(spacing: 8) {
                    ProgressView()
                    Text(verbatim: workingText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            snapButton
            Button("Finish without a receipt") { isConfirmingWithoutReceipt = true }
                .frame(minHeight: 44)
                .confirmationDialog("Finish without a receipt?", isPresented: $isConfirmingWithoutReceipt, titleVisibility: .visible) {
                    Button("Finish") { Task { await finish(receipt: nil) } }
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text(verbatim: ShoppingWords.finishWithoutReceiptMessage(itemCount: model.items.count))
                }
        }
        .disabled(model.isWorking || model.items.isEmpty)
        .readableWidth()
        .padding(.horizontal)
        .padding(.top, 12)
        .padding(.bottom, 8)
        .background(.bar)
        // The buttons stop growing before they cover the cart under them.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    /// A tap takes the picture; the menu, held, also offers the photo
    /// library. Without a camera, a tap opens the menu.
    @ViewBuilder
    private var snapButton: some View {
        let label = Label("Snap the receipt", systemImage: "camera").frame(maxWidth: .infinity)
        Group {
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Menu {
                    ShoppingReceiptSourceChoices(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture)
                } label: {
                    label
                } primaryAction: {
                    isTakingReceiptPicture = true
                }
            } else {
                Menu {
                    ShoppingReceiptSourceChoices(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture)
                } label: {
                    label
                }
            }
        }
        .menuStyle(.button)
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
    }

    private func finish(receipt file: PendingFile?) async {
        guard let text = await model.finish(receipt: file) else { return }
        onFinished(text)
        dismiss()
    }
}

/// ShoppingCartRow is an item in the cart: a filled check that takes it out
/// again, its picture, what and how many, and about what it costs.
struct ShoppingCartRow: View {
    let item: ShoppingItem
    let showsListName: Bool
    let onTakeOut: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onTakeOut) {
                ShoppingCheckMark(isChecked: true)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Take \(item.itemDescription) out of the cart"))
            if !dynamicTypeSize.isAccessibilitySize {
                ShoppingPictureView(item: item, size: 44)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: item.shownTitle)
                if showsListName {
                    Text(verbatim: item.listName)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                // At the largest sizes the price goes under the words, which
                // otherwise break mid-word beside it.
                if dynamicTypeSize.isAccessibilitySize {
                    priceText
                }
            }
            Spacer(minLength: 8)
            if !dynamicTypeSize.isAccessibilitySize {
                priceText
            }
        }
    }

    @ViewBuilder
    private var priceText: some View {
        let price = ShoppingMoney.shownLineTotal(of: item)
        if !price.isEmpty {
            Text(verbatim: price)
                .font(.subheadline)
                .monospacedDigit()
                .foregroundStyle(.secondary)
        }
    }
}

/// ShoppingResumeRow offers to resume a trip the person finished in the last
/// few hours, for a Done tapped by mistake: what it bought goes back in the
/// cart.
struct ShoppingResumeRow: View {
    let trip: ShoppingTrip
    let onResume: () async -> Void
    @State private var isResuming = false

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) {
                words
                Spacer(minLength: 8)
                resumeButton
            }
            VStack(alignment: .leading, spacing: 8) {
                words
                resumeButton
            }
        }
        .padding(.vertical, 4)
    }

    private var words: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: ShoppingWords.finishedLine(trip))
                .font(.subheadline)
            Text(verbatim: ShoppingWords.resumeDetail(trip))
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    private var resumeButton: some View {
        Button {
            isResuming = true
            Task {
                await onResume()
                isResuming = false
            }
        } label: {
            Text("Resume")
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .disabled(isResuming)
    }
}

/// ShoppingNowRow is the person's own trip on the Shopping screen: the store
/// and how many items are in their cart. It opens the store.
struct ShoppingNowRow: View {
    let trip: ShoppingTrip
    let cartCount: Int

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: ShoppingWords.shoppingNowLine(trip))
                Text(verbatim: ShoppingWords.inYourCart(cartCount))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: "cart.fill")
                .foregroundStyle(Color.accentColor)
        }
    }
}
